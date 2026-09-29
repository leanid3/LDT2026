"use client";

import Link from "next/link";
import { FormEvent, useState } from "react";
import { useRouter } from "next/navigation";
import { apiRequest } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import type { ObjectRecord } from "@/lib/types";

export default function NewObjectPage() {
  const { token } = useAuth(); const router = useRouter();
  const [name, setName] = useState(""); const [address, setAddress] = useState(""); const [customer, setCustomer] = useState(""); const [contractor, setContractor] = useState(""); const [permit, setPermit] = useState("");
  const [busy, setBusy] = useState(false); const [error, setError] = useState("");
  async function submit(event: FormEvent<HTMLFormElement>) { event.preventDefault(); if (!token) return; setBusy(true); setError(""); try { const result = await apiRequest<ObjectRecord>("/objects", token, { method: "POST", body: JSON.stringify({ name: name.trim(), address: address.trim() || undefined, customer: customer.trim() || undefined, contractor: contractor.trim() || undefined, permit_number: permit.trim() || undefined }) }); router.push("/objects/" + result.id); } catch (err) { setError(err instanceof Error ? err.message : "Не удалось создать объект."); } finally { setBusy(false); } }
  return <>
    <div className="breadcrumbs"><Link href="/objects">Объекты</Link><span>/</span><strong>Новый объект</strong></div>
    <div className="page-head"><div><div className="page-eyebrow">КАРТОЧКА ОБЪЕКТА</div><h1 className="page-title">Новый объект</h1><p className="page-subtitle">Укажите исходные сведения. Позже их можно будет использовать в документах проверки.</p></div></div>
    <div className="form-layout">
      <section className="surface form-card">
        <div className="section-heading"><div><span className="section-number">01</span><div><h2>Основные сведения</h2><p>Название обязательно, остальные поля можно заполнить позже.</p></div></div></div>
        {error && <div className="alert alert-error"><span>!</span>{error}</div>}
        <form onSubmit={submit}>
          <div className="form-grid">
            <label className="form-field-wide"><span className="field-label">Название объекта <b className="required-mark">*</b></span><input className="text-input" autoFocus maxLength={240} value={name} onChange={(event) => setName(event.target.value)} placeholder="Например, жилой комплекс «Северный парк»" required/></label>
            <label className="form-field-wide"><span className="field-label">Адрес</span><input className="text-input" value={address} onChange={(event) => setAddress(event.target.value)} placeholder="Город, улица, дом"/></label>
            <label><span className="field-label">Заказчик</span><input className="text-input" value={customer} onChange={(event) => setCustomer(event.target.value)} placeholder="Название организации"/></label>
            <label><span className="field-label">Подрядчик</span><input className="text-input" value={contractor} onChange={(event) => setContractor(event.target.value)} placeholder="Название организации"/></label>
            <label className="form-field-wide"><span className="field-label">Номер разрешения на строительство</span><input className="text-input" value={permit} onChange={(event) => setPermit(event.target.value)} placeholder="Например, RU77-000000-00"/></label>
          </div>
          <div className="form-actions"><Link href="/objects" className="button button-secondary">Отмена</Link><button type="submit" className="button button-primary" disabled={!name.trim() || busy}>{busy ? <><span className="spinner"/> Создаем…</> : "Создать объект"}</button></div>
        </form>
      </section>
      <aside className="form-aside"><div className="aside-art"><span className="art-circle art-circle-one"/><span className="art-circle art-circle-two"/><span className="art-line art-line-one"/><span className="art-line art-line-two"/><span className="art-building"><i/><i/><i/><i/><i/><i/></span><span className="art-base"/></div><div className="aside-copy"><span className="page-eyebrow">ДАЛЬШЕ</span><h3>Документы и проверка</h3><p>После создания объекта можно загрузить ПД, РД и ИД, приложить реестр и запустить анализ.</p><div className="aside-tiny-list"><span>01 <b>Добавить комплект документов</b></span><span>02 <b>Проверить выводы системы</b></span><span>03 <b>Сформировать протокол</b></span></div></div></aside>
    </div>
  </>;
}
