require('dotenv').config();
const express = require('express');
const session = require('express-session');
const cors = require('cors');
const path = require('path');
const db = require('./db');
const { startPoller } = require('./services/poller');

const app = express();
const IS_PRODUCTION = process.env.NODE_ENV === 'production';

app.use(cors({ origin: true, credentials: true }));
app.use(express.json());
app.use(express.urlencoded({ extended: true }));

// Trust proxy for Railway/Render (needed for secure cookies behind HTTPS)
if (IS_PRODUCTION) {
  app.set('trust proxy', 1);
}

app.use(session({
  name: 'mailflow.sid',
  secret: process.env.SESSION_SECRET || 'dev-secret-change-me',
  resave: false,
  saveUninitialized: false,
  proxy: IS_PRODUCTION,
  cookie: {
    secure: IS_PRODUCTION ? 'auto' : false,
    httpOnly: true,
    maxAge: 7 * 24 * 60 * 60 * 1000,
    sameSite: 'lax',
  },
}));

// ─── Routes ───────────────────────────────────────────────────────────────────
app.use(require('./routes/auth'));
app.use(require('./routes/threads'));

// Manual poll trigger
app.post('/api/poll', async (req, res) => {
  const { pollAllUsers } = require('./services/poller');
  await pollAllUsers();
  res.json({ ok: true });
});

// Health check for Railway/Render
app.get('/health', (req, res) => res.json({ ok: true, env: process.env.NODE_ENV }));

// ─── Serve frontend ───────────────────────────────────────────────────────────
app.use(express.static(path.join(__dirname, '../frontend')));
app.get('*', (req, res) => {
  res.sendFile(path.join(__dirname, '../frontend/index.html'));
});

// ─── Start ────────────────────────────────────────────────────────────────────
const PORT = process.env.PORT || 3000;

db._init().then(() => {
  app.listen(PORT, () => {
    console.log(`\n🚀 MailFlow running → http://localhost:${PORT}`);
    console.log(`🌍 Environment: ${process.env.NODE_ENV || 'development'}`);
    console.log(`📬 Gmail OAuth → http://localhost:${PORT}/auth/google\n`);
    startPoller(60000);
  });
}).catch(err => {
  console.error('❌ Failed to initialize database:', err);
  process.exit(1);
});
