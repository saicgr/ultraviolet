import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { api, CopilotMessage } from "../lib/api";
import { Button, Card, CardHeader, ErrorState, Input } from "../components/ui";
import { useActiveCustomer } from "../lib/customer";

export function Copilot() {
  const customer = useActiveCustomer();
  const cid = customer.data?.id ?? "";
  const [messages, setMessages] = useState<CopilotMessage[]>([]);
  const [input, setInput] = useState("");
  const [suggestions, setSuggestions] = useState<{ id: string; label: string }[]>([]);

  const chat = useMutation({
    mutationFn: (next: CopilotMessage[]) => api.copilotChat({ customer_id: cid, messages: next }),
    onSuccess: (res) => {
      setMessages((cur) => [...cur, { role: "assistant", content: res.message }]);
      setSuggestions(res.suggested_actions ?? []);
    },
  });

  function send(text: string) {
    if (!text.trim() || !cid) return;
    const next: CopilotMessage[] = [...messages, { role: "user", content: text }];
    setMessages(next);
    setInput("");
    setSuggestions([]);
    chat.mutate(next);
  }

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Copilot</h1>
      <Card>
        <CardHeader title="Chat" description="Natural-language assistant grounded in your catalog and query history." />
        <div className="space-y-3 max-h-[440px] overflow-y-auto pr-1">
          {messages.length === 0 && (
            <p className="text-sm text-foreground/60">Ask about tables, costs, lineage, or "rewrite this query".</p>
          )}
          {messages.map((m, i) => (
            <div key={i} className={`rounded-md p-3 text-sm ${m.role === "user" ? "bg-accent/20 ml-12" : "bg-muted/30 mr-12"}`}>
              <div className="text-xs uppercase tracking-wide text-foreground/50 mb-1">{m.role}</div>
              <p className="whitespace-pre-wrap">{m.content}</p>
            </div>
          ))}
          {chat.isPending && <div className="text-xs text-foreground/50 animate-pulse">Thinking…</div>}
        </div>
        {suggestions.length > 0 && (
          <div className="mt-3 flex flex-wrap gap-2">
            {suggestions.map((s) => (
              <Button key={s.id} size="sm" variant="outline" onClick={() => send(s.label)}>{s.label}</Button>
            ))}
          </div>
        )}
        <div className="mt-4 flex gap-2">
          <Input
            placeholder="What's our most expensive table this week?"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter") send(input); }}
          />
          <Button onClick={() => send(input)} disabled={!input.trim() || chat.isPending}>Send</Button>
        </div>
        {chat.isError && <div className="mt-3"><ErrorState error={chat.error} /></div>}
      </Card>
    </div>
  );
}
