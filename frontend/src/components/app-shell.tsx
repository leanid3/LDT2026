"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { useAuth } from "@/lib/auth";

function BrandMark() { return <span className="brand-mark" aria-hidden="true"><i /><i /><i /></span>; }
function Icon({ name }: { name: "grid" | "layers" | "chevron" | "menu" | "close" | "exit" | "search" | "plus" | "arrow" }) {
  const paths: Record<string, React.ReactNode> = {
    grid: <><rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/></>,
    layers: <><path d="m12 3 9 5-9 5-9-5 9-5Z"/><path d="m3 12 9 5 9-5M3 16l9 5 9-5"/></>,
    chevron: <path d="m9 18 6-6-6-6"/>, menu: <><path d="M4 7h16M4 12h16M4 17h16"/></>, close: <><path d="m6 6 12 12M18 6 6 18"/></>, exit: <><path d="M10 17l5-5-5-5M15 12H3"/><path d="M12 3h6a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-6"/></>, search: <><circle cx="11" cy="11" r="7"/><path d="m20 20-4-4"/></>, plus: <><path d="M12 5v14M5 12h14"/></>, arrow: <><path d="M5 12h14M13 6l6 6-6 6"/></>
  };
  return <svg className="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{paths[name]}</svg>;
}

function initials(name: string) { return name.split(/\s+/).slice(0, 2).map((part) => part[0]).join("").toUpperCase(); }

export function AppShell({ children }: { children: React.ReactNode }) {
  const { user, ready, signOut } = useAuth();
  const router = useRouter();
  const pathname = usePathname();
  const [menuOpen, setMenuOpen] = useState(false);
  useEffect(() => { if (ready && !user && pathname !== "/login") router.replace("/login"); }, [ready, user, pathname, router]);
  useEffect(() => { setMenuOpen(false); }, [pathname]);

  if (!ready || !user) return <div className="boot-screen"><BrandMark /><span>Открываем рабочее пространство</span></div>;
  const activeObjects = pathname.startsWith("/objects") || pathname.startsWith("/checks");
  return <div className="app-frame">
    <aside className={"sidebar " + (menuOpen ? "sidebar-open" : "")}>
      <Link href="/objects" className="brand"><BrandMark /><span><strong>инспектор</strong><small>ИИ · строительный надзор</small></span></Link>
      <div className="side-caption">РАБОЧЕЕ ПРОСТРАНСТВО</div>
      <nav className="side-nav" aria-label="Основная навигация">
        <Link className={activeObjects ? "nav-link active" : "nav-link"} href="/objects"><Icon name="grid"/><span>Объекты</span><Icon name="chevron"/></Link>
        <Link className={pathname.startsWith("/parameters") ? "nav-link active" : "nav-link"} href="/parameters"><Icon name="layers"/><span>Матрица параметров</span><Icon name="chevron"/></Link>
      </nav>
      <div className="sidebar-spacer" />
      
      <button className="profile-button" onClick={signOut}><span className="avatar">{initials(user.full_name)}</span><span className="profile-copy"><strong>{user.full_name}</strong><small>{user.role === "inspector" ? "Инспектор" : user.role}</small></span><Icon name="exit"/></button>
      <div className="sidebar-foot">ИНСПЕКТОР ИИ <span>·</span> 2026</div>
    </aside>
    {menuOpen && <button className="mobile-scrim" onClick={() => setMenuOpen(false)} aria-label="Закрыть меню" />}
    <div className="main-column">
      <header className="topbar"><button className="mobile-menu-button" aria-label={menuOpen ? "Закрыть меню" : "Открыть меню"} onClick={() => setMenuOpen(!menuOpen)}><Icon name={menuOpen ? "close" : "menu"}/></button><div className="topbar-crumb"><span>Рабочее пространство</span><b>/</b><strong>{pathname.startsWith("/parameters") ? "Матрица параметров" : "Проверка объектов"}</strong></div><div className="topbar-right"><span className="connection-dot"/><span>Локальный стенд</span><span className="avatar avatar-small">{initials(user.full_name)}</span></div></header>
      <main className="page-content">{children}</main>
      <footer className="app-footer"><span>Инспектор ИИ</span><span>Сверка документов с Матрицей требований</span></footer>
    </div>
  </div>;
}

export function ButtonIcon({ name }: { name: "plus" | "arrow" | "search" }) { return <Icon name={name}/>; }
