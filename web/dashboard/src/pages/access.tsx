import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, fmtTime } from "@/lib/api";
import {
  Badge,
  Button,
  Card,
  ErrorState,
  Loading,
  PageHeader,
} from "@/components/ui";
export function Access() {
  const [login, setLogin] = useState("");
  const client = useQueryClient();
  const members = useQuery({
    queryKey: ["members"],
    queryFn: () => api<any[]>("/api/v1/access/members"),
  });
  const invite = useMutation({
    mutationFn: () =>
      api<{ invite_url: string }>("/api/v1/access/invitations", {
        method: "POST",
        body: JSON.stringify({ github_login: login }),
      }),
    onSuccess: () => {
      setLogin("");
      void client.invalidateQueries({ queryKey: ["members"] });
    },
  });
  if (members.isLoading) return <Loading />;
  if (members.error) return <ErrorState error={members.error} />;
  return (
    <>
      <PageHeader
        eyebrow="Workspace security"
        title="Access"
        description="GitHub identities and workspace roles. Members can operate runs; owners also manage access and hosting."
      />
      <div className="two-column">
        <Card>
          <h2>Members</h2>
          <div className="member-list">
            {members.data?.map((m) => (
              <div key={m.id}>
                {m.avatar_url ? (
                  <img src={m.avatar_url} alt="" />
                ) : (
                  <span className="avatar">{m.login[0]?.toUpperCase()}</span>
                )}
                <div>
                  <strong>@{m.login}</strong>
                  <small>Joined {fmtTime(m.created_at)}</small>
                </div>
                <Badge status={m.role} />
              </div>
            ))}
          </div>
        </Card>
        <Card>
          <h2>Invite a member</h2>
          <label>
            GitHub username
            <input
              value={login}
              onChange={(e) => setLogin(e.target.value)}
              placeholder="octocat"
            />
          </label>
          <p className="muted">
            Invitations expire after seven days and only the matching GitHub
            identity can accept them.
          </p>
          <Button
            disabled={!login.trim() || invite.isPending}
            onClick={() => invite.mutate()}
          >
            Create invitation
          </Button>
          {invite.data && (
            <div className="token-box">
              <strong>One-time invitation link</strong>
              <code>{invite.data.invite_url}</code>
            </div>
          )}
          {invite.error && <ErrorState error={invite.error} />}
        </Card>
      </div>
    </>
  );
}
