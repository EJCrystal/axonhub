import { createFileRoute } from '@tanstack/react-router';
import { RouteGuard } from '@/components/route-guard';
import IntelligenceManagement from '@/features/intelligence';

function ProtectedIntelligence() {
  return (
    <RouteGuard requiredScopes={['read_channels']} scopeLevel="system">
      <IntelligenceManagement />
    </RouteGuard>
  );
}

export const Route = createFileRoute('/_authenticated/intelligence/')({
  component: ProtectedIntelligence,
});
