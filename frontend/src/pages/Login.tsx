import { useState } from "react";
import { Button, Card, CardHeader, ErrorState, Input } from "../components/ui";

export function Login() {
  const [key, setKey] = useState("");
  const [err, setErr] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    setBusy(true);
    setErr(null);
    try {
      const r = await fetch("/api/v1/auth/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ api_key: key }),
      });
      if (!r.ok) throw new Error(`login ${r.status}`);
      const body = await r.json();
      localStorage.setItem("uv_token", body.access_token);
      window.location.href = "/";
    } catch (e) {
      setErr(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="max-w-md mx-auto pt-24">
      <Card>
        <CardHeader title="Sign in" description="Use an Ultraviolet API key to authenticate the dashboard." />
        <div className="space-y-3">
          <Input
            placeholder="uvk_..."
            type="password"
            value={key}
            onChange={(e) => setKey(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && submit()}
          />
          <Button onClick={submit} disabled={!key || busy} className="w-full">
            {busy ? "Signing in…" : "Sign in"}
          </Button>
          {err ? <ErrorState error={err} /> : null}
        </div>
      </Card>
    </div>
  );
}
