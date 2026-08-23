import { CheckCircleOutlined, DatabaseOutlined, FileOutlined, HddOutlined, LaptopOutlined, WarningOutlined } from "@ant-design/icons";
import { Alert, Card, Col, Row, Skeleton, Statistic, Tag, Typography } from "antd";
import { useCallback, useEffect, useState } from "react";
import type { AdminAPI } from "../api";
import { PageTitle } from "../components/PageTitle";
import { formatBytes } from "../format";
import type { DoctorReport, ServerStats } from "../types";

export function DashboardPage({ api }: { api: AdminAPI }) {
  const [stats, setStats] = useState<ServerStats>();
  const [doctor, setDoctor] = useState<DoctorReport>();
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [nextStats, nextDoctor] = await Promise.all([api.stats(), api.doctor()]);
      setStats(nextStats);
      setDoctor(nextDoctor);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason));
    } finally {
      setLoading(false);
    }
  }, [api]);
  useEffect(() => { void load(); }, [load]);

  return <>
    <PageTitle title="概览" description="查看同步服务的容量、设备和数据健康状态。" onRefresh={() => void load()} refreshing={loading} />
    {error && <Alert type="error" showIcon message="读取服务状态失败" description={error} />}
    {loading && !stats ? <Skeleton active /> : <Row gutter={[16, 16]}>
      <Metric title="Vault" value={stats?.vaults ?? 0} icon={<DatabaseOutlined />} />
      <Metric title="活跃设备" value={stats?.active_devices ?? 0} icon={<LaptopOutlined />} />
      <Metric title="当前文件" value={stats?.current_files ?? 0} icon={<FileOutlined />} />
      <Metric title="逻辑数据量" value={formatBytes(stats?.logical_bytes ?? 0)} icon={<HddOutlined />} />
      <Metric title="修订记录" value={stats?.revisions ?? 0} />
      <Metric title="去重块" value={`${stats?.chunks ?? 0} / ${formatBytes(stats?.chunk_bytes ?? 0)}`} />
    </Row>}
    <Card className="section-card" title="数据健康">
      {doctor?.ok ? <Alert type="success" showIcon icon={<CheckCircleOutlined />} message="数据库与分块文件检查正常" description={`SQLite integrity_check: ${doctor.integrity}`} /> :
        <Alert type="warning" showIcon icon={<WarningOutlined />} message="检测到需要处理的数据问题" description={`缺失 ${doctor?.missing_chunk_hashes?.length ?? 0}，损坏 ${doctor?.corrupt_chunk_hashes?.length ?? 0}，孤立 ${doctor?.orphan_chunk_files?.length ?? 0}`} />}
      <Typography.Paragraph className="health-note"><Tag color="blue">本机管理端</Tag>此页面不会经由 Cloudflare Tunnel 暴露。</Typography.Paragraph>
    </Card>
  </>;
}

function Metric({ title, value, icon }: { title: string; value: string | number; icon?: React.ReactNode }) {
  return <Col xs={24} sm={12} xl={8}><Card className="metric-card"><Statistic title={title} value={value} prefix={icon} /></Card></Col>;
}
