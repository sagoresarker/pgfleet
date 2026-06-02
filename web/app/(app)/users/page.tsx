"use client";

import { PageHeader } from "@/components/shell";
import { Badge, Button, Card, CardBody, CardHeader, CardTitle, Field, Input, Select, Spinner } from "@/components/ui";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { UserPlus } from "lucide-react";
import { useState } from "react";

export default function UsersPage() {
  const { user } = useAuth();
  const qc = useQueryClient();
  const isAdmin = user?.role === "admin";

  const users = useQuery({ queryKey: ["users"], queryFn: api.listUsers, enabled: isAdmin });

  const toggle = useMutation({
    mutationFn: ({ id, disable }: { id: string; disable: boolean }) =>
      disable ? api.disableUser(id) : api.enableUser(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });

  if (!isAdmin) {
    return (
      <div className="rise">
        <PageHeader title="Access" />
        <Card>
          <CardBody className="py-12 text-center text-sm text-fg-muted">User management requires the admin role.</CardBody>
        </Card>
      </div>
    );
  }

  return (
    <div className="rise grid gap-6 lg:grid-cols-3">
      <div className="lg:col-span-2">
        <PageHeader title="Access" subtitle="Manage who can operate the control plane." />
        <Card>
          <CardHeader>
            <CardTitle>Users</CardTitle>
            {users.isFetching && <Spinner />}
          </CardHeader>
          <CardBody className="p-0">
            {users.isLoading ? (
              <div className="grid place-items-center py-12">
                <Spinner className="h-6 w-6" />
              </div>
            ) : (
              <ul className="divide-y divide-line">
                {(users.data?.users ?? []).map((u) => (
                  <li key={u.id} className="flex items-center gap-4 px-5 py-3.5">
                    <div className="grid h-8 w-8 place-items-center rounded-full border border-line-bright bg-ink-800 font-mono text-xs uppercase text-azure">
                      {u.email.slice(0, 2)}
                    </div>
                    <div className="flex-1">
                      <div className="text-sm text-fg">{u.email}</div>
                    </div>
                    <Badge tone={u.role === "admin" ? "violet" : u.role === "operator" ? "azure" : "neutral"}>{u.role}</Badge>
                  </li>
                ))}
              </ul>
            )}
          </CardBody>
        </Card>
      </div>

      <div className="mt-[88px]">
        <CreateUserCard />
      </div>
    </div>
  );
}

function CreateUserCard() {
  const qc = useQueryClient();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState("viewer");
  const [error, setError] = useState<string | null>(null);

  const create = useMutation({
    mutationFn: () => api.createUser({ email, password, role }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["users"] });
      setEmail("");
      setPassword("");
      setError(null);
    },
    onError: (e) => setError(e instanceof Error ? e.message : "Failed"),
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle>Add user</CardTitle>
        <UserPlus className="h-4 w-4 text-fg-faint" />
      </CardHeader>
      <CardBody className="space-y-4">
        <Field label="Email">
          <Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="ops@example.com" />
        </Field>
        <Field label="Password" hint="Min 8 characters.">
          <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </Field>
        <Field label="Role">
          <Select value={role} onChange={(e) => setRole(e.target.value)}>
            <option value="viewer">viewer · read-only</option>
            <option value="operator">operator · manage instances</option>
            <option value="admin">admin · full access</option>
          </Select>
        </Field>
        {error && <div className="rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">{error}</div>}
        <Button
          className="w-full"
          onClick={() => create.mutate()}
          disabled={create.isPending || !email || password.length < 8}
        >
          {create.isPending ? "Creating…" : "Create user"}
        </Button>
      </CardBody>
    </Card>
  );
}
