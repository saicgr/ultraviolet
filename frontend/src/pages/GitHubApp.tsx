import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useActiveCustomer } from "../lib/customer";
import { Badge, Button, Card, CardHeader, EmptyState, ErrorState, SkeletonRows, Table, TD, TH, THead, TR } from "../components/ui";

export function GitHubApp() {
  const customer = useActiveCustomer();
  const qc = useQueryClient();

  const info = useQuery({
    queryKey: ["gh-install-info", customer.data?.id],
    queryFn: () => api.githubInstallInfo(),
    enabled: !!customer.data,
  });
  const installs = useQuery({
    queryKey: ["gh-installations", customer.data?.id],
    queryFn: () => api.listGitHubInstallations(),
    enabled: !!customer.data,
  });
  const repos = useQuery({
    queryKey: ["gh-repos", customer.data?.id],
    queryFn: () => api.listGitHubRepos(),
    enabled: !!customer.data,
  });

  const connect = useMutation({
    mutationFn: (full: string) => api.connectGitHubRepo(full),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["gh-repos", customer.data?.id] }),
  });
  const disconnect = useMutation({
    mutationFn: (full: string) => api.disconnectGitHubRepo(full),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["gh-repos", customer.data?.id] }),
  });

  if (!customer.data) return <EmptyState title="No active customer" />;

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">GitHub App</h1>

      <Card>
        <CardHeader title="Installation" description="Install the Ultraviolet GitHub App on your org to analyze PRs." />
        {info.isPending && <SkeletonRows count={1} />}
        {info.isError && <ErrorState error={info.error} onRetry={() => info.refetch()} />}
        {info.data && (
          <div className="flex items-center gap-3">
            {info.data.installed ? <Badge variant="success">Installed</Badge> : <Badge variant="warn">Not installed</Badge>}
            {info.data.install_url ? (
              <a href={info.data.install_url} target="_blank" rel="noreferrer">
                <Button size="sm">{info.data.installed ? "Manage on GitHub ↗" : "Install GitHub App ↗"}</Button>
              </a>
            ) : (
              <span className="text-xs text-foreground/60">
                Set UV_GITHUB_APP_SLUG on the API to enable the install link.
              </span>
            )}
          </div>
        )}
      </Card>

      <Card>
        <CardHeader title="Installations" description="GitHub orgs/users where the App is installed for this workspace." />
        {installs.isPending && <SkeletonRows count={2} />}
        {installs.isError && <ErrorState error={installs.error} onRetry={() => installs.refetch()} />}
        {installs.data?.length === 0 && <EmptyState title="No installations yet" hint="Install the App above, then refresh." />}
        {!!installs.data?.length && (
          <Table>
            <THead><TR><TH>Account</TH><TH>Installation ID</TH><TH>Status</TH></TR></THead>
            <tbody>
              {installs.data.map((i) => (
                <TR key={i.installation_id}>
                  <TD className="font-mono">{i.account_login || "—"}</TD>
                  <TD className="tabular-nums">{i.installation_id}</TD>
                  <TD>{i.suspended ? <Badge variant="danger">suspended</Badge> : <Badge variant="success">active</Badge>}</TD>
                </TR>
              ))}
            </tbody>
          </Table>
        )}
      </Card>

      <Card>
        <CardHeader title="Repositories" description="Connect a repo to enable PR impact analysis for it." />
        {repos.isPending && <SkeletonRows count={3} />}
        {repos.isError && <ErrorState error={repos.error} onRetry={() => repos.refetch()} />}
        {repos.data?.length === 0 && <EmptyState title="No repositories" hint="Add repos to the installation on GitHub." />}
        {!!repos.data?.length && (
          <Table>
            <THead><TR><TH>Repository</TH><TH>Status</TH><TH /></TR></THead>
            <tbody>
              {repos.data.map((r) => (
                <TR key={r.repo_id}>
                  <TD className="font-mono">{r.full_name}</TD>
                  <TD>{r.connected ? <Badge variant="success">connected</Badge> : <Badge>disconnected</Badge>}</TD>
                  <TD>
                    {r.connected ? (
                      <Button size="sm" variant="outline" onClick={() => disconnect.mutate(r.full_name)} disabled={disconnect.isPending}>
                        Disconnect
                      </Button>
                    ) : (
                      <Button size="sm" onClick={() => connect.mutate(r.full_name)} disabled={connect.isPending}>
                        Connect
                      </Button>
                    )}
                  </TD>
                </TR>
              ))}
            </tbody>
          </Table>
        )}
      </Card>
    </div>
  );
}
