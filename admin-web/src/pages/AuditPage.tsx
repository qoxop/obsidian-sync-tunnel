import { Alert, InputNumber, Table, Tag, Typography } from "antd";
import { useCallback, useEffect, useState } from "react";
import type { AdminAPI } from "../api";
import { PageTitle } from "../components/PageTitle";
import { formatTime, shortID } from "../format";
import type { AuditEvent } from "../types";

export function AuditPage({ api }: { api: AdminAPI }) {
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [limit, setLimit] = useState(100);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const load = useCallback(async () => {
    setLoading(true); setError("");
    try { setEvents(await api.audit(limit)); }
    catch (reason) { setError(reason instanceof Error ? reason.message : String(reason)); }
    finally { setLoading(false); }
  }, [api, limit]);
  useEffect(() => { void load(); }, [load]);
  return <>
    <PageTitle title="审计日志" description="查看 Vault、设备、维护和认证相关的管理事件。" onRefresh={() => void load()} refreshing={loading} action={<InputNumber min={10} max={1000} value={limit} onChange={value => setLimit(value ?? 100)} addonAfter="条" />} />
    {error && <Alert type="error" showIcon message="读取审计日志失败" description={error} />}
    <Table rowKey="id" loading={loading} dataSource={events} scroll={{ x: 900 }} columns={[
      { title: "时间", dataIndex: "created_at", width: 190, render: formatTime },
      { title: "事件", dataIndex: "event_type", render: (value: string) => <Tag color="blue">{value}</Tag> },
      { title: "Vault", dataIndex: "vault_id", render: shortID },
      { title: "设备", dataIndex: "device_id", render: shortID },
      { title: "执行者", dataIndex: "actor" },
      { title: "详情", dataIndex: "details", render: (value: Record<string, unknown>) => <Typography.Text code>{JSON.stringify(value)}</Typography.Text> }
    ]} />
  </>;
}
