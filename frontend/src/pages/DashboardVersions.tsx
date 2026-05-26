import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import { api } from "../lib/api";
import {
  Button,
  Card,
  CardHeader,
  EmptyState,
  ErrorState,
  SkeletonRows,
  Table,
  TD,
  TH,
  THead,
  TR,
} from "../components/ui";

export function DashboardVersions() {
  const qc = useQueryClient();
  const [params] = useSearchParams();
  const dashboardId = params.get("dashboard_id") ?? "";
  const versions = useQuery({
    queryKey: ["dashboard-versions", dashboardId],
    queryFn: () => api.dashboardVersions(dashboardId),
    enabled: !!dashboardId,
  });
  const restore = useMutation({
    mutationFn: (versionId: string) => api.restoreDashboardVersion(dashboardId, versionId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["dashboard-versions"] }),
  });

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Dashboard Versions</h1>
      {!dashboardId && (
        <EmptyState
          title="No dashboard selected"
          hint="Add ?dashboard_id=... to the URL."
        />
      )}
      {dashboardId && (
        <Card>
          <CardHeader title={`Versions for ${dashboardId}`} />
          {versions.isPending && <SkeletonRows count={4} />}
          {versions.isError && <ErrorState error={versions.error} onRetry={() => versions.refetch()} />}
          {versions.data && versions.data.length === 0 && <EmptyState title="No versions" />}
          {versions.data && versions.data.length > 0 && (
            <Table>
              <THead>
                <TR>
                  <TH>Version</TH>
                  <TH>Author</TH>
                  <TH>Note</TH>
                  <TH>Created</TH>
                  <TH />
                </TR>
              </THead>
              <tbody>
                {versions.data.map((v) => (
                  <TR key={v.id}>
                    <TD>v{v.version}</TD>
                    <TD>{v.author ?? "—"}</TD>
                    <TD>{v.note ?? "—"}</TD>
                    <TD>{new Date(v.created_at).toLocaleString()}</TD>
                    <TD>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => restore.mutate(v.id)}
                        disabled={restore.isPending}
                      >
                        Restore
                      </Button>
                    </TD>
                  </TR>
                ))}
              </tbody>
            </Table>
          )}
          {restore.isError && <div className="mt-3"><ErrorState error={restore.error} /></div>}
        </Card>
      )}
    </div>
  );
}
