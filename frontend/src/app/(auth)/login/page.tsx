"use client";

import { FormEvent, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { useAuth } from "@/lib/auth";
import { ApiError } from "@/lib/api";

function Mark() { return <span className="brand-mark"><i/><i/><i/></span>; }
export default function LoginPage() {
  const { signIn, user, ready } = useAuth();
  const router = useRouter();
  const [login, setLogin] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [requestId, setRequestId] = useState("");
  useEffect(() => { if (ready && user) router.replace("/objects"); }, [ready, user, router]);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setBusy(true); setError(""); setRequestId("");
    try { await signIn(login.trim(), password); router.replace("/objects"); }
    catch (err) { setError(err instanceof Error ? err.message : "Не удалось войти."); if (err instanceof ApiError && err.requestId) setRequestId(err.requestId); }
    finally { setBusy(false); }
  }
  return <div className="login-page">
    <aside className="login-aside">
      <Link href="/login" className="brand"><Mark/><span><strong>инспектор</strong><small>ИИ · строительный надзор</small></span></Link>
      <div className="login-copy"><div className="login-kicker"><i/>Контроль строительных решений</div><h1>Внимание к деталям. Уверенность в каждом выводе.</h1><p>Сопоставляйте проектную, рабочую и исполнительную документацию, проверяйте доказательства и формируйте протокол в одном рабочем пространстве.</p></div>
      <div className="login-aside-foot">ПД · РД · ИД <span>—</span> единая картина соответствия</div>
    </aside>
    <section className="login-panel"><div className="login-card">
      <div className="brand"><Mark/><span><strong>инспектор</strong><small>ИИ · строительный надзор</small></span></div>
      <div className="page-eyebrow">РАБОЧИЙ ДОСТУП</div><h2>С возвращением</h2><p>Войдите, чтобы открыть объекты и продолжить проверки.</p>
      {error && <div className="alert alert-error" role="alert"><span>!</span><div>{error}{requestId && <div className="error-detail">Номер обращения: {requestId}</div>}</div></div>}
      <form className="login-form" onSubmit={submit}>
        <label><span className="field-label">Логин</span><input className="text-input" autoComplete="username" value={login} onChange={(event) => setLogin(event.target.value)} placeholder="Введите логин" required autoFocus/></label>
        <label><span className="field-label">Пароль</span><input className="text-input" type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} placeholder="Введите пароль" required/></label>
        <button className="button button-primary" type="submit" disabled={busy}>{busy ? <><span className="spinner"/> Проверяем доступ…</> : <>Войти в систему <span>→</span></>}</button>
      </form>
      <div className="login-footnote"><span className="secure-mark">⌑</span> Защищенное подключение · Доступ выдает администратор системы</div>
    </div></section>
  </div>;
}
