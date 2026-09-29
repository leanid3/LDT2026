"use client";

import Link from "next/link";
import { useCallback, useEffect, useMemo, useState } from "react";
import { ButtonIcon } from "@/components/app-shell";
import { apiRequest, formatDate } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import type { ObjectList, ObjectRecord } from "@/lib/types";

function BuildingGlyph({ size = 40 }: { size?: number }) { return <svg width={size} height={size} style={{ width: size, height: size, flex: "none" }} viewBox="0 0 40 40" fill="none" aria-hidden="true"><path d="M9 33V11.5L20 6l11 5.5V33M15 16h2m6 0h2M15 21h2m6 0h2M17 33v-7h6v7" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round"/><path d="M5 33h30" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round"/></svg>; }
function ObjectCard({ item }: { item: ObjectRecord }) {
  return <Link className="object-card surface" href={"/objects/" + item.id}>
    <div className="object-card-top"><span className="object-icon"><BuildingGlyph size={23}/></span><span className="object-created">Добавлен {formatDate(item.created_at)}</span></div>
    <h2>{item.name}</h2>
    <p className="object-address">{item.address || "Адрес не указан"}</p>
    <div className="object-card-details">
      <div><span>Заказчик</span><strong>{item.customer || "Не указан"}</strong></div>
      <div><span>Номер разрешения</span><strong>{item.permit_number || "—"}</strong></div>
    </div>
    <div className="object-card-bottom"><span>Открыть карточку</span><span className="object-arrow"><ButtonIcon name="arrow"/></span></div>
  </Link>;
}

export default function ObjectsPage() {
  const { token } = useAuth();
  const [objects, setObjects] = useState<ObjectRecord[]>([]);
  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const refresh = useCallback(async () => {
    if (!token) return;
    setLoading(true); setError("");
    try { const result = await apiRequest<ObjectList>("/objects", token); setObjects(result.items || []); }
    catch (err) { setError(err instanceof Error ? err.message : "Не удалось загрузить объекты."); }
    finally { setLoading(false); }
  }, [token]);
  useEffect(() => { refresh(); }, [refresh]);
  const filtered = useMemo(() => {
    const normalized = query.trim().toLocaleLowerCase("ru-RU");
    if (!normalized) return objects;
    return objects.filter((item) => [item.name, item.address, item.customer, item.contractor, item.permit_number].some((value) => value?.toLocaleLowerCase("ru-RU").includes(normalized)));
  }, [objects, query]);
  const latest = objects.length ? [...objects].sort((a, b) => b.created_at.localeCompare(a.created_at))[0] : null;
  return <>
    <div className="page-head">
      <div><div className="page-eyebrow">УПРАВЛЕНИЕ ПРОВЕРКАМИ</div><h1 className="page-title">Объекты</h1><p className="page-subtitle">Строительные объекты и история проверок документации.</p></div>
      <Link href="/objects/new" className="button button-primary"><ButtonIcon name="plus"/> Добавить объект</Link>
    </div>
    <section className="overview-strip">
      <div className="overview-stat"><span className="stat-icon stat-icon-teal"><BuildingGlyph size={22}/></span><div><small>ВСЕГО ОБЪЕКТОВ</small><strong>{loading ? "—" : objects.length}</strong></div></div>
      <div className="overview-divider"/>
      <div className="overview-stat overview-stat-wide"><span className="stat-mark">↗</span><div><small>ПОСЛЕДНИЙ ДОБАВЛЕННЫЙ</small><strong>{latest ? latest.name : "Пока нет объектов"}</strong><em>{latest ? formatDate(latest.created_at) : "Создайте первый объект"}</em></div></div>
      <div className="overview-strip-right"><span className="live-indicator"/><span>Подключение к API</span><b>●</b></div>
    </section>
    <div className="list-toolbar"><div><h2>Рабочий список</h2><span>{loading ? "Загрузка…" : filtered.length + " " + plural(filtered.length, "объект", "объекта", "объектов")}</span></div><label className="search-box"><ButtonIcon name="search"/><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Название, адрес или заказчик" aria-label="Поиск по загруженным объектам"/><kbd>⌘ K</kbd></label></div>
    {error && <div className="alert alert-error" role="alert"><span>!</span><div>{error}<div className="error-detail">Проверьте, запущен ли API на localhost:8080.</div><button className="inline-action" onClick={refresh}>Повторить загрузку</button></div></div>}
    {loading ? <div className="object-grid">{[1,2,3,4].map((i) => <div className="object-skeleton surface" key={i}><span/><span/><span/><span/></div>)}</div> : filtered.length ? <div className="object-grid">{filtered.map((item) => <ObjectCard item={item} key={item.id}/>)}</div> : query ? <div className="surface empty-state"><span className="empty-symbol"><ButtonIcon name="search"/></span><h3>Ничего не найдено</h3><p>Попробуйте изменить запрос. Поиск работает по уже загруженным объектам.</p><button className="button button-secondary" onClick={() => setQuery("")}>Сбросить поиск</button></div> : <div className="surface empty-state"><span className="empty-symbol"><BuildingGlyph size={23}/></span><h3>Добавьте первый объект</h3><p>Создайте карточку объекта, чтобы загрузить документацию и запустить первую проверку.</p><Link href="/objects/new" className="button button-primary"><ButtonIcon name="plus"/> Создать объект</Link></div>}
    <div className="page-note"><span>i</span> Проверки и документы хранятся внутри карточки соответствующего объекта.</div>
  </>;
}
function plural(value: number, one: string, few: string, many: string) { const n = Math.abs(value) % 100; const last = n % 10; if (n > 10 && n < 20) return many; if (last > 1 && last < 5) return few; if (last === 1) return one; return many; }
