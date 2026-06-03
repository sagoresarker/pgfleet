"use client";

import { PageHeader } from "@/components/shell";
import {
  ActionMenu,
  ActionMenuItem,
  Badge,
  Button,
  Card,
  CardBody,
  CardHeader,
  CardTitle,
  ConfirmDialog,
  EmptyState,
  Field,
  Input,
  Modal,
  PasswordInput,
  Select,
  SkeletonRows,
  StatusLed,
  useToast,
} from "@/components/ui";
import { api, type Role, type User } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Ban, MoreHorizontal, ShieldCheck, UserPlus, Users as UsersIcon } from "lucide-react";
import { useState } from "react";

function roleTone(role: Role): "violet" | "azure" | "neutral" {
  return role === "admin" ? "violet" : role === "operator" ? "azure" : "neutral";
}

export default function UsersPage() {
  const { user } = useAuth();
  const isAdmin = user?.role === "admin";

  const users = useQuery({ queryKey: ["users"], queryFn: api.listUsers, enabled: isAdmin });
  const [createOpen, setCreateOpen] = useState(false);

  if (!isAdmin) {
    return (
      <div className="rise">
        <PageHeader title="Access" subtitle="Manage who can operate the control plane." />
        <Card>
          <CardBody className="py-4">
            <EmptyState
              icon={<ShieldCheck className="h-5 w-5" />}
              title="Admin access required"
              description="User management is restricted to administrators. Ask an admin to grant you the role."
            />
          </CardBody>
        </Card>
      </div>
    );
  }

  const list = users.data?.users ?? [];

  return (
    <div className="rise">
      <PageHeader
        title="Access"
        subtitle="Manage who can operate the control plane."
        action={
          <Button onClick={() => setCreateOpen(true)}>
            <UserPlus className="h-4 w-4" />
            Add user
          </Button>
        }
      />

      <Card>
        <CardHeader>
          <CardTitle>Users</CardTitle>
          {list.length > 0 && <Badge tone="neutral">{list.length}</Badge>}
        </CardHeader>
        <CardBody className="p-0">
          {users.isLoading ? (
            <div className="p-5">
              <SkeletonRows rows={4} />
            </div>
          ) : list.length === 0 ? (
            <EmptyState
              icon={<UsersIcon className="h-5 w-5" />}
              title="No users yet"
              description="Add a teammate so they can sign in and operate the fleet."
              action={
                <Button size="sm" onClick={() => setCreateOpen(true)}>
                  <UserPlus className="h-4 w-4" />
                  Add first user
                </Button>
              }
            />
          ) : (
            <ul className="divide-y divide-line">
              {list.map((u) => (
                <UserRow key={u.id} u={u} isSelf={u.id === user?.id} />
              ))}
            </ul>
          )}
        </CardBody>
      </Card>

      <CreateUserModal open={createOpen} onOpenChange={setCreateOpen} />
    </div>
  );
}

function UserRow({ u, isSelf }: { u: User; isSelf: boolean }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [disableOpen, setDisableOpen] = useState(false);
  const [enableOpen, setEnableOpen] = useState(false);

  const toggle = useMutation({
    mutationFn: ({ id, disable }: { id: string; disable: boolean }) =>
      disable ? api.disableUser(id) : api.enableUser(id),
    onSuccess: (_d, vars) => {
      qc.invalidateQueries({ queryKey: ["users"] });
      toast.push(
        vars.disable ? `Disabled ${u.email}` : `Enabled ${u.email}`,
        vars.disable ? "danger" : "healthy",
      );
      setDisableOpen(false);
      setEnableOpen(false);
    },
    onError: (e) => toast.push(e instanceof Error ? e.message : "Action failed", "danger"),
  });

  return (
    <li className="flex items-center gap-4 px-5 py-3.5">
      <div className="grid h-9 w-9 shrink-0 place-items-center rounded-full border border-line-bright bg-ink-800 font-mono text-xs uppercase text-azure">
        {u.email.slice(0, 2)}
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-sm text-fg" title={u.email}>
            {u.email}
          </span>
          {isSelf && <Badge tone="neutral">you</Badge>}
        </div>
        <div className="mt-0.5 flex items-center gap-1.5 font-mono text-[11px] text-fg-faint">
          <StatusLed status="healthy" />
          active
        </div>
      </div>
      <Badge tone={roleTone(u.role)}>{u.role}</Badge>

      <ActionMenu
        trigger={
          <Button size="sm" variant="ghost" aria-label={`Actions for ${u.email}`}>
            <MoreHorizontal className="h-4 w-4" />
          </Button>
        }
      >
        <ActionMenuItem
          icon={<ShieldCheck className="h-4 w-4" />}
          onSelect={() => setEnableOpen(true)}
          disabled={toggle.isPending}
        >
          Enable access
        </ActionMenuItem>
        <ActionMenuItem
          icon={<Ban className="h-4 w-4" />}
          danger
          disabled={isSelf || toggle.isPending}
          onSelect={() => setDisableOpen(true)}
        >
          Disable access
        </ActionMenuItem>
      </ActionMenu>

      <ConfirmDialog
        open={disableOpen}
        onOpenChange={setDisableOpen}
        title={`Disable ${u.email}?`}
        description="This user will be signed out and can no longer access the control plane until re-enabled. Make sure you are not locking out the last administrator."
        danger
        confirmLabel="Disable access"
        loading={toggle.isPending}
        onConfirm={() => toggle.mutate({ id: u.id, disable: true })}
      />
      <ConfirmDialog
        open={enableOpen}
        onOpenChange={setEnableOpen}
        title={`Enable ${u.email}?`}
        description="Restore this user's ability to sign in and operate the fleet with their existing role."
        confirmLabel="Enable access"
        loading={toggle.isPending}
        onConfirm={() => toggle.mutate({ id: u.id, disable: false })}
      />
    </li>
  );
}

function CreateUserModal({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const qc = useQueryClient();
  const toast = useToast();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState<Role>("viewer");
  const [error, setError] = useState<string | null>(null);

  function reset() {
    setEmail("");
    setPassword("");
    setRole("viewer");
    setError(null);
  }

  const create = useMutation({
    mutationFn: () => api.createUser({ email: email.trim(), password, role }),
    onSuccess: (res) => {
      qc.invalidateQueries({ queryKey: ["users"] });
      toast.push(`Created ${res.user.email}`, "healthy");
      reset();
      onOpenChange(false);
    },
    onError: (e) => setError(e instanceof Error ? e.message : "Could not create user"),
  });

  const emailValid = /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim());
  const valid = emailValid && password.length >= 8;

  function submit() {
    setError(null);
    if (!valid) {
      setError("Provide a valid email and a password of at least 8 characters.");
      return;
    }
    create.mutate();
  }

  return (
    <Modal
      open={open}
      onOpenChange={(o) => {
        onOpenChange(o);
        if (!o) reset();
      }}
      title="Add user"
      description="Create a sign-in for a teammate and assign their control-plane role."
      footer={
        <>
          <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)} disabled={create.isPending}>
            Cancel
          </Button>
          <Button size="sm" loading={create.isPending} disabled={!valid} onClick={submit}>
            <UserPlus className="h-4 w-4" />
            Create user
          </Button>
        </>
      }
    >
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
        noValidate
      >
        <Field label="Email">
          <Input
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder="ops@example.com"
            autoComplete="off"
            autoFocus
            required
            aria-required="true"
          />
        </Field>
        <Field label="Password" hint="At least 8 characters. Share it securely with the user.">
          <PasswordInput
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="new-password"
            required
            aria-required="true"
          />
        </Field>
        <Field label="Role" hint="Viewers read-only; operators manage instances; admins manage everything.">
          <Select value={role} onChange={(e) => setRole(e.target.value as Role)}>
            <option value="viewer">viewer · read-only</option>
            <option value="operator">operator · manage instances</option>
            <option value="admin">admin · full access</option>
          </Select>
        </Field>
        {error && (
          <div
            role="alert"
            aria-live="assertive"
            className="rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger"
          >
            {error}
          </div>
        )}
        {/* Hidden submit so Enter submits the form inside the modal. */}
        <button type="submit" className="sr-only" aria-hidden tabIndex={-1} />
      </form>
    </Modal>
  );
}
