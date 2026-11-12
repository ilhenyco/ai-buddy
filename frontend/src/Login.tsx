import { useEffect, useState } from 'react';
import { SignIn, WarningCircle } from '@phosphor-icons/react';
import { api, errorText, type Person, type SessionContext } from './api';
import { Spinner } from './ui';
import './login.css';

// Demo login instead of SSO (server DEMO_MODE). Roles come from the server's assignments.
const HINTS: Record<string, string> = {
  'u-alex': 'Руководитель · команда platform',
  'u-dasha': 'Бадди · команда platform',
  'u-sasha': 'Стажёр · команда platform',
  'u-masha': 'Стажёр · команда platform',
  'u-ilya': 'Стажёр · команда platform',
  'u-irina': 'Руководитель · команда mobile',
  'u-oleg': 'Бадди · команда mobile',
  'u-kirill': 'Стажёр · команда mobile',
  'u-anna': 'HR · без назначений',
};

export function Login({ onLogin }: { onLogin: (ctx: SessionContext) => void }) {
  const [users, setUsers] = useState<Person[] | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState('');

  function load() {
    setError('');
    api.demoUsers().then(r => setUsers(r.items)).catch(e => setError(errorText(e)));
  }
  useEffect(load, []);

  async function login(id: string) {
    setBusy(id); setError('');
    try { onLogin(await api.login(id)); } catch (e) { setError(errorText(e)); } finally { setBusy(''); }
  }

  // Main demo team first: lead, buddy, interns; then the second team and HR.
  const order = Object.keys(HINTS);
  const rank = (id: string) => (order.indexOf(id) + 1) || order.length + 1;
  const ordered = users && [...users].sort((a, b) => rank(a.id) - rank(b.id));
  return <main className="prototype-root login-page">
    <section className="login-card" aria-labelledby="login-title">
      <img className="login-mascot" src="/assets/homych-team.png" alt="" />
      <p className="eyebrow">Хомыч · демо-вход</p>
      <h1 id="login-title">Кто входит?</h1>
      <p className="muted">Корпоративный вход ещё не подключён. Выберите тестового пользователя — права и назначения берутся с сервера.</p>
      {error && <div role="alert" className="error-banner"><WarningCircle size={20} /><span>{error}</span><button className="button" onClick={load}>Повторить</button></div>}
      {!ordered && !error && <div className="login-loading"><Spinner label="Загружаем пользователей…" /></div>}
      {ordered && <ul className="login-users">{ordered.map(u => <li key={u.id}>
        <button className="login-user" onClick={() => login(u.id)} disabled={!!busy} aria-label={`Войти как ${u.displayName}`}>
          <span><strong>{u.displayName}</strong><small>{HINTS[u.id] ?? u.id}</small></span>
          {busy === u.id ? <Spinner label="Входим" /> : <SignIn size={20} />}
        </button>
      </li>)}</ul>}
    </section>
  </main>;
}
