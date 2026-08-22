import { Alert, InputNumber, Table, Tag, Typography } from "antd";
import { useCallback, useEffect, useState } from "react";
import type { AdminAPI } from "../api";
import { PageTitle } from "../components/PageTitle";
import type { ServerLogEntry } from "../types";

const standardKeys = new Set(["time", "level", "msg"]);

export function LogsPage({ api }: { api: AdminAPI }) {
  const [entries, setEntries] = useState<ServerLogEntry[]>([]);
  const [limit, setLimit] = useState(200);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try { setEntries(await api.logs(limit)); }
    catch (reason) { setError(reason instanceof Error ? reason.message : String(reason)); }
    finally { setLoading(false); }
  }, [api, limit]);
  useEffect(() => { void load(); }, [load]);

  return <>
    <PageTitle title="运行日志" description="直接查看服务最近的运行状态，不需要打开命令行。" onRefresh={() => void load()} refreshing={loading} action={<InputNumber min={20} max={1000} value={limit} onChange={value => setLimit(value ?? 200)} addonAfter="条" />} />
    {error && <Alert type="error" showIcon message="读取运行日志失败" description={error} />}
    <Table rowKey={(_, index) => String(index)} loading={loading} dataSource={entries} scroll={{ x: 900 }} columns={[
      { title: "时间", dataIndex: "time", width: 220, render: (value?: string) => value ? new Date(value).toLocaleString("zh-CN") : "—" },
      { title: "级别", dataIndex: "level", width: 90, render: (value?: string) => <Tag color={value === "ERROR" ? "red" : value === "WARN" ? "orange" : "blue"}>{value ?? "INFO"}</Tag> },
      { title: "消息", dataIndex: "msg", width: 220 },
      { title: "详情", render: (_: unknown, row: ServerLogEntry) => {
        const details = Object.fromEntries(Object.entries(row).filter(([key]) => !standardKeys.has(key)));
        return Object.keys(details).length ? <Typography.Text code>{JSON.stringify(details)}</Typography.Text> : "—";
      } }
    ]} />
  </>;
}
