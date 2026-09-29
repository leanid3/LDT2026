"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { apiRequest } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import type { ObjectRecord, ProcessDetail } from "@/lib/types";

type Props = {
  current: string;
  objectId?: string;
  processId?: string;
  processAncestor?: boolean;
  protocolAncestor?: boolean;
};

type ObjectContext = { id: string; name: string };

export function WorkspaceBreadcrumbs({ current, objectId, processId, processAncestor = false, protocolAncestor = false }: Props) {
  const { token } = useAuth();
  const [object, setObject] = useState<ObjectContext | null>(null);

  useEffect(() => {
    if (!token) return;
    let active = true;
    async function loadContext() {
      try {
        let id = objectId;
        if (processId) {
          const process = await apiRequest<ProcessDetail>(`/processes/${processId}`, token);
          id = process.object_id;
        }
        if (!id) return;
        const result = await apiRequest<ObjectRecord>(`/objects/${id}`, token);
        if (active) setObject({ id: result.id, name: result.name });
      } catch {
        if (active) setObject(null);
      }
    }
    void loadContext();
    return () => { active = false; };
  }, [objectId, processId, token]);

  return <nav className="breadcrumbs hierarchical-breadcrumbs" aria-label="Навигационная цепочка">
    <Link href="/objects">Объекты</Link>
    {object && <><span aria-hidden="true">/</span><Link className="breadcrumb-object" href={`/objects/${object.id}`} title={object.name}>{object.name}</Link></>}
    {processId && processAncestor && <><span aria-hidden="true">/</span><Link href={`/checks/${processId}`}>Проверка документов</Link></>}
    {processId && protocolAncestor && <><span aria-hidden="true">/</span><Link href={`/checks/${processId}/protocol`}>Протокол</Link></>}
    <span aria-hidden="true">/</span><strong title={current}>{current}</strong>
  </nav>;
}