require('dotenv').config();
const express = require('express');
const { v4: uuid } = require('uuid');
const { google } = require('googleapis');
const db = require('../db');

const router = express.Router();

// ─── Build OAuth2 client ──────────────────────────────────────────────────────
function getOAuth2Client(tokens = null) {
  const client = new google.auth.OAuth2(
    process.env.GMAIL_CLIENT_ID,
    process.env.GMAIL_CLIENT_SECRET,
    process.env.GMAIL_REDIRECT_URI
  );
  if (tokens) client.setCredentials(tokens);
  return client;
}

// ─── Step 1: Redirect to Google ───────────────────────────────────────────────
router.get('/auth/google', (req, res) => {
  const client = getOAuth2Client();
  const url = client.generateAuthUrl({
    access_type: 'offline',
    prompt: 'consent',
    scope: [
      'https://www.googleapis.com/auth/gmail.send',
      'https://www.googleapis.com/auth/gmail.readonly',
      'https://www.googleapis.com/auth/gmail.modify',
      'https://www.googleapis.com/auth/userinfo.email',
      'https://www.googleapis.com/auth/userinfo.profile',
    ],
  });
  res.redirect(url);
});

// ─── Step 2: Handle OAuth callback ───────────────────────────────────────────
router.get('/auth/callback', async (req, res) => {
  const { code, error } = req.query;
  if (error) return res.redirect('/?error=oauth_denied');

  try {
    const client = getOAuth2Client();
    let tokens;
    try {
      const tokenResponse = await client.getToken(code);
      tokens = tokenResponse.tokens;
    } catch (tokenErr) {
      console.error('OAuth token exchange error:', {
        message: tokenErr.message,
        code: tokenErr.code,
        status: tokenErr.response?.status,
        data: tokenErr.response?.data,
        redirectUri: process.env.GMAIL_REDIRECT_URI,
      });
      return res.redirect('/?error=oauth_token_exchange_failed');
    }

    client.setCredentials(tokens);

    // Get user profile
    const oauth2 = google.oauth2({ version: 'v2', auth: client });
    let profile;
    try {
      const profileResponse = await oauth2.userinfo.get();
      profile = profileResponse.data;
    } catch (profileErr) {
      console.error('OAuth profile fetch error:', {
        message: profileErr.message,
        code: profileErr.code,
        status: profileErr.response?.status,
        data: profileErr.response?.data,
      });
      return res.redirect('/?error=oauth_profile_failed');
    }

    // Upsert user
    let userId;
    try {
      const existing = db.prepare('SELECT * FROM users WHERE email = ?').get(profile.email);

      if (existing) {
        db.prepare('UPDATE users SET name = ?, gmail_token = ? WHERE id = ?')
          .run(profile.name, JSON.stringify({ ...tokens, email: profile.email }), existing.id);
        userId = existing.id;
      } else {
        userId = uuid();
        db.prepare('INSERT INTO users (id, email, name, gmail_token) VALUES (?, ?, ?, ?)')
          .run(userId, profile.email, profile.name, JSON.stringify({ ...tokens, email: profile.email }));
      }
    } catch (dbErr) {
      console.error('OAuth user upsert error:', {
        message: dbErr.message,
        code: dbErr.code,
      });
      return res.redirect('/?error=oauth_user_save_failed');
    }

    req.session.regenerate((regenerateErr) => {
      if (regenerateErr) {
        console.error('Session regenerate error:', regenerateErr.message);
        return res.redirect('/?error=session_failed');
      }

      req.session.userId = userId;
      req.session.save((saveErr) => {
        if (saveErr) {
          console.error('Session save error:', saveErr.message);
          return res.redirect('/?error=session_failed');
        }
        res.redirect('/');
      });
    });
  } catch (err) {
    console.error('OAuth error:', err.message);
    res.redirect('/?error=oauth_failed');
  }
});

// ─── Current user ─────────────────────────────────────────────────────────────
router.get('/api/me', (req, res) => {
  if (!req.session?.userId) return res.json({ user: null });
  const user = db.prepare('SELECT id, email, name FROM users WHERE id = ?').get(req.session.userId);
  res.json({ user: user || null });
});

router.get('/api/auth-debug', (req, res) => {
  const userId = req.session?.userId || null;
  const user = userId
    ? db.prepare('SELECT id, email, name FROM users WHERE id = ?').get(userId)
    : null;

  res.json({
    hasCookieHeader: Boolean(req.headers.cookie),
    hasSessionUserId: Boolean(userId),
    userExists: Boolean(user),
    sessionIdPrefix: req.sessionID ? req.sessionID.slice(0, 8) : null,
    protocol: req.protocol,
    secure: req.secure,
    forwardedProto: req.get('x-forwarded-proto') || null,
    nodeEnv: process.env.NODE_ENV || null,
  });
});

router.get('/api/oauth-config-debug', (req, res) => {
  const clientId = process.env.GMAIL_CLIENT_ID || '';
  const redirectUri = process.env.GMAIL_REDIRECT_URI || '';

  res.json({
    hasClientId: Boolean(clientId),
    clientIdPrefix: clientId ? clientId.slice(0, 12) : null,
    clientIdSuffix: clientId ? clientId.slice(-24) : null,
    hasClientSecret: Boolean(process.env.GMAIL_CLIENT_SECRET),
    redirectUri,
    redirectUriHasWhitespace: redirectUri !== redirectUri.trim(),
    nodeEnv: process.env.NODE_ENV || null,
  });
});

router.post('/api/logout', (req, res) => {
  req.session.destroy(() => {
    res.clearCookie('mailflow.sid');
    res.json({ ok: true });
  });
});

module.exports = router;
