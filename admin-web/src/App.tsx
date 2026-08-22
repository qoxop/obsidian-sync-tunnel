import { AuditOutlined, CloudServerOutlined, DatabaseOutlined, DashboardOutlined, FileTextOutlined, LogoutOutlined, MenuFoldOutlined, MenuUnfoldOutlined, SafetyCertificateOutlined, ToolOutlined } from "@ant-design/icons";
import { Alert, Button, ConfigProvider, Layout, Menu, Result, Spin, Typography, theme } from "antd";
import zhCN from "antd/locale/zh_CN";
import { useEffect, useMemo, useState } from "react";
import { AdminAPI, AdminAPIError, getAdminSession } from "./api";
import { AuditPage } from "./pages/AuditPage";
import { ConnectivityPage } from "./pages/ConnectivityPage";
import { DashboardPage } from "./pages/DashboardPage";
import { LoginPage } from "./pages/LoginPage";
import { LogsPage } from "./pages/LogsPage";
import { MaintenancePage } from "./pages/MaintenancePage";
import { VaultsPage } from "./pages/VaultsPage";
import type { AdminSession } from "./types";

const tokenKey = "sync-tunnel-admin-token";
type Page = "dashboard" | "vaults" | "connectivity" | "audit" | "logs" | "maintenance";

export default function App() {
  const [session, setSession] = useState<AdminSession>();
  const [token, setToken] = useState("");
  const [page, setPage] = useState<Page>("dashboard");
  const [collapsed, setCollapsed] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const api = useMemo(() => new AdminAPI(token), [token]);

  useEffect(() => {
    void getAdminSession().then(next => {
      setSession(next);
      if (next.authentication === "token") setToken(sessionStorage.getItem(tokenKey) ?? "");
    }).catch(reason => setError(reason instanceof Error ? reason.message : String(reason))).finally(() => setLoading(false));
  }, []);

  const login = async (value: string) => {
    setLoading(true); setError("");
    try { await new AdminAPI(value).stats(); sessionStorage.setItem(tokenKey, value); setToken(value); }
    catch (reason) { setError(reason instanceof Error ? reason.message : String(reason)); }
    finally { setLoading(false); }
  };
  const logout = () => { sessionStorage.removeItem(tokenKey); setToken(""); };

  if (loading && !session) return <div className="centered"><Spin size="large" tip="正在连接本机服务"><div className="spin-space" /></Spin></div>;
  if (!session) return <Result status="error" title="无法打开管理中心" subTitle={error} extra={<Button onClick={() => location.reload()}>重试</Button>} />;
  if (session.authentication === "token" && !token) return <LoginPage loading={loading} error={error} onLogin={login} />;

  const content = renderPage(page, api);
  return <ConfigProvider locale={zhCN} theme={{ algorithm: theme.defaultAlgorithm, token: { colorPrimary: "#6d4aff", borderRadius: 12, colorBgLayout: "#f5f6fa" } }}>
    <Layout className="app-layout">
      <Layout.Sider collapsible collapsed={collapsed} trigger={null} width={240} theme="light" className="app-sider">
        <div className="app-brand"><span className="brand-icon-small"><SafetyCertificateOutlined /></span>{!collapsed && <div><Typography.Text strong>Sync Tunnel</Typography.Text><small>本机管理中心</small></div>}</div>
        <Menu mode="inline" selectedKeys={[page]} onClick={({ key }) => setPage(key as Page)} items={[
          { key: "dashboard", icon: <DashboardOutlined />, label: "概览" },
          { key: "vaults", icon: <DatabaseOutlined />, label: "Vault 与设备" },
          { key: "connectivity", icon: <CloudServerOutlined />, label: "连接诊断" },
          { key: "audit", icon: <AuditOutlined />, label: "审计日志" },
          { key: "logs", icon: <FileTextOutlined />, label: "运行日志" },
          { key: "maintenance", icon: <ToolOutlined />, label: "维护与备份" }
        ]} />
        <Button className="collapse-button" type="text" icon={collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />} onClick={() => setCollapsed(value => !value)} />
      </Layout.Sider>
      <Layout>
        <Layout.Header className="app-header">
          <Alert className="local-badge" type="success" showIcon message="仅本机访问" />
          {session.authentication === "token" && <Button icon={<LogoutOutlined />} onClick={logout}>退出</Button>}
        </Layout.Header>
        <Layout.Content className="app-content">{content}</Layout.Content>
      </Layout>
    </Layout>
  </ConfigProvider>;
}

function renderPage(page: Page, api: AdminAPI) {
  switch (page) {
    case "vaults": return <VaultsPage api={api} />;
    case "connectivity": return <ConnectivityPage api={api} />;
    case "audit": return <AuditPage api={api} />;
    case "logs": return <LogsPage api={api} />;
    case "maintenance": return <MaintenancePage api={api} />;
    default: return <DashboardPage api={api} />;
  }
}

export function isUnauthorized(reason: unknown): boolean {
  return reason instanceof AdminAPIError && reason.status === 401;
}
