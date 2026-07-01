import * as React from "react";

interface State {
  error: Error | null;
}

/**
 * ErrorBoundary stops a render-time crash in one page from unmounting the whole
 * SPA (sidebar + all). It shows the error + a Reload, and resets when the route
 * changes (via the `resetKey` prop) so navigating away recovers without a hard
 * refresh.
 */
export class ErrorBoundary extends React.Component<
  { children: React.ReactNode; resetKey?: string },
  State
> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidUpdate(prev: { resetKey?: string }) {
    if (prev.resetKey !== this.props.resetKey && this.state.error) {
      this.setState({ error: null });
    }
  }

  render() {
    if (this.state.error) {
      return (
        <div className="rounded-lg border border-danger/40 bg-danger/5 p-6">
          <h2 className="text-lg font-semibold text-danger">Something went wrong on this page</h2>
          <p className="mt-1 text-sm text-foreground/70">
            The rest of the app is still working — use the sidebar to navigate, or reload.
          </p>
          <pre className="mt-3 max-h-48 overflow-auto rounded-md bg-background p-3 text-xs text-foreground/70">
            {this.state.error.message}
          </pre>
          <button
            className="mt-4 inline-flex h-9 items-center rounded-md border border-border px-3 text-sm hover:bg-muted"
            onClick={() => this.setState({ error: null })}
          >
            Try again
          </button>
        </div>
      );
    }
    return this.props.children;
  }
}
