import { Button, Flex, Typography } from "antd";
import type { ReactNode } from "react";

interface PageTitleProps {
  title: string;
  description: string;
  action?: ReactNode;
  onRefresh?: () => void;
  refreshing?: boolean;
}

export function PageTitle({ title, description, action, onRefresh, refreshing }: PageTitleProps) {
  return (
    <Flex justify="space-between" align="start" gap={16} wrap>
      <div>
        <Typography.Title level={2} className="page-title">{title}</Typography.Title>
        <Typography.Paragraph type="secondary">{description}</Typography.Paragraph>
      </div>
      <Flex gap={8} wrap>
        {onRefresh && <Button onClick={onRefresh} loading={refreshing}>刷新</Button>}
        {action}
      </Flex>
    </Flex>
  );
}
