import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router-dom";
import { FormEvent, useRef, useState } from "react";
import { api, fmtTime } from "@/lib/api";
import { Badge, Button, Card, Empty, ErrorState, Loading, PageHeader } from "@/components/ui";

type Conversation = { id: string; agent_id: string; title: string; status: string; updated_at: string };
type Agent = { id: string; name: string; state: string };
type AgentRun = { id: string; status: string; output_json?: string; terminal_error?: string };
type Message = { id: string; sequence: number; role: string; content: { text?: string }; created_at: string; run?: AgentRun; citations: string[]; action_ledger_url?: string };
type Detail = { conversation: Conversation; messages: Message[]; runs: AgentRun[] };

export function Conversations() {
  const { id } = useParams();
  const navigate = useNavigate();
  const client = useQueryClient();
  const [title, setTitle] = useState("");
  const [agentID, setAgentID] = useState("");
  const [message, setMessage] = useState("");
  const messageKey = useRef(crypto.randomUUID());
  const conversations = useQuery({ queryKey: ["conversations"], queryFn: () => api<{ conversations: Conversation[] }>("/api/v1/conversations") });
  const agents = useQuery({ queryKey: ["conversation-agents"], queryFn: () => api<{ agents: Agent[] }>("/api/v1/agents") });
  const detail = useQuery({ queryKey: ["conversation", id], queryFn: () => api<Detail>(`/api/v1/conversations/${id}`), enabled: !!id, refetchInterval: id ? 2500 : false });
  const create = useMutation({
    mutationFn: () => api<Conversation>("/api/v1/conversations", { method: "POST", body: JSON.stringify({ title, agent_id: agentID }) }),
    onSuccess: (value) => { void client.invalidateQueries({ queryKey: ["conversations"] }); navigate(`/conversations/${value.id}`); },
  });
  const send = useMutation({
    mutationFn: () => api(`/api/v1/conversations/${id}/messages`, { method: "POST", headers: { "Idempotency-Key": messageKey.current }, body: JSON.stringify({ message }) }),
    onSuccess: () => { setMessage(""); messageKey.current = crypto.randomUUID(); void client.invalidateQueries({ queryKey: ["conversation", id] }); void client.invalidateQueries({ queryKey: ["conversations"] }); },
  });
  const submitCreate = (event: FormEvent) => { event.preventDefault(); create.mutate(); };
  const submitMessage = (event: FormEvent) => { event.preventDefault(); send.mutate(); };
  const availableAgents = (agents.data?.agents || []).filter((agent) => agent.state === "active");

  return <>
    <PageHeader eyebrow="Shared workspace" title="Conversations" description="Choose a durable agent, keep messages ordered, and follow every run and citation back to persisted evidence." />
    {(conversations.error || agents.error) && <ErrorState error={conversations.error || agents.error} />}
    <div className="conversation-layout">
      <div className="conversation-sidebar">
        <Card>
          <h2>Start a conversation</h2>
          <form onSubmit={submitCreate}>
            <label>Title<input value={title} onChange={(event) => setTitle(event.target.value)} placeholder="Morning priorities" /></label>
            <label>Agent<select value={agentID} onChange={(event) => setAgentID(event.target.value)}><option value="">Select an agent</option>{availableAgents.map((agent) => <option key={agent.id} value={agent.id}>{agent.name}</option>)}</select></label>
            {create.error && <ErrorState error={create.error} />}
            <Button disabled={!title.trim() || !agentID || create.isPending}>{create.isPending ? "Starting…" : "Start"}</Button>
          </form>
        </Card>
        <Card className="conversation-list">
          <h2>Recent</h2>
          {conversations.isLoading && <Loading label="Loading conversations" />}
          {conversations.data?.conversations.length === 0 && <Empty title="No conversations" detail="Choose an agent above to begin." />}
          {conversations.data?.conversations.map((conversation) => <Link className={conversation.id === id ? "active" : ""} key={conversation.id} to={`/conversations/${conversation.id}`}><strong>{conversation.title}</strong><small>{fmtTime(conversation.updated_at)}</small></Link>)}
        </Card>
      </div>
      <Card className="conversation-thread">
        {!id && <Empty title="Select a conversation" detail="Messages, run status, citations, and action-ledger links appear here." />}
        {id && detail.isLoading && <Loading label="Loading conversation" />}
        {detail.error && <ErrorState error={detail.error} />}
        {detail.data && <>
          <div className="card-heading"><div><h2>{detail.data.conversation.title}</h2><p className="muted">Agent {detail.data.conversation.agent_id}</p></div><Badge status={detail.data.conversation.status} /></div>
          <div className="message-list">
            {detail.data.messages.map((item) => <article className="message-card" key={item.id}>
              <div className="card-heading"><strong>#{item.sequence} {item.role}</strong><small>{fmtTime(item.created_at)}</small></div>
              <p>{item.content.text}</p>
              {item.run && <div className="run-inline"><Link to={`/agent-runs/${item.run.id}`}>{item.run.id}</Link><Badge status={item.run.status} /></div>}
              {item.run?.terminal_error && <p className="bad">{item.run.terminal_error}</p>}
              {item.run?.output_json && <pre className="json-block">{item.run.output_json}</pre>}
              {!!item.citations.length && <div className="citation-list"><strong>Citations</strong>{item.citations.map((citation) => <Link key={citation} to={`/knowledge?citation=${encodeURIComponent(citation)}`}>{citation}</Link>)}</div>}
              {item.action_ledger_url && <Link className="entity-link" to={item.action_ledger_url}>View action ledger</Link>}
            </article>)}
          </div>
          <form className="composer" onSubmit={submitMessage}><textarea value={message} onChange={(event) => { setMessage(event.target.value); messageKey.current = crypto.randomUUID(); }} placeholder="Ask this agent…" />{send.error && <ErrorState error={send.error} />}<div className="composer-actions"><small>Creates one durable, traceable agent run.</small><Button disabled={!message.trim() || send.isPending}>{send.isPending ? "Sending…" : "Send"}</Button></div></form>
        </>}
      </Card>
    </div>
  </>;
}
