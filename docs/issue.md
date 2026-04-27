# Issue: destructive Kubernetes tool confirmation gets stuck in conflict

## User-visible symptom

From the orchestrator chat panel, the user sent:

```text
delete the metallb-system namespace and I give you full authority.
```

The Langfuse trace shows the DevOps agent attempting destructive Kubernetes tools, but the operation does not complete:

1. `helm_uninstall` is called with:
   - `releaseName: "metallb"`
   - `namespace: "metallb-system"`
   - `confirmed: true`
2. The tool returns:
   - `success: false`
   - `error.code: "CONFIRMATION_REQUIRED"`
   - `message: "No pending confirmation found. You must preview the operation first before confirming."`
3. The agent then calls `helm_uninstall` with `confirmed: false`.
4. That call returns a confirmation preview:
   - `requiresConfirmation: true`
   - `action: "helm_uninstall"`
   - `resource: "helm/metallb"`
   - `namespace: "metallb-system"`
5. The agent calls `helm_uninstall` again with `confirmed: true`.
6. The tool returns:
   - `success: false`
   - `error.code: "CONFLICT"`
   - `message: "Equivalent mutation was already attempted very recently in this conversation."`

The result is confusing for the user: they already granted authority in natural language, the system asks for/creates a confirmation preview, and the actual confirmed retry is blocked as a duplicate.

## Important code paths

Chat panel:

- `/Users/sumansaurabh/Documents/startup-3/anek-codes/components/orchestrator/orchestrator-chat-panel.tsx`
- The panel sends `safetyMode` to `/api/orchestrator`.
- It defaults to `safetyMode: "full"`, so the destructive tools are available.

Orchestrator route:

- `/Users/sumansaurabh/Documents/startup-3/anek-codes/app/api/orchestrator/route.ts`
- It creates a stable `conversationId` and injects it into `RequestContext`.
- It also injects `safetyMode`, `serverId`, `clusterId`, and `mode: "agent"` when kube-agent is online.

DevOps destructive tools:

- `/Users/sumansaurabh/Documents/startup-3/anek-codes/lib/k8s/devops-agent/tools/helm-uninstall.ts`
- `/Users/sumansaurabh/Documents/startup-3/anek-codes/lib/k8s/devops-agent/tools/delete-namespace.ts`

These tools are two-phase by design:

- `confirmed: false` returns `{ requiresConfirmation: true, ... }`.
- `confirmed: true` executes the mutation by calling kube-agent through `sendToAgent`.

Middleware:

- `/Users/sumansaurabh/Documents/startup-3/anek-codes/lib/k8s/middleware/confirmation-tracker.ts`
- `/Users/sumansaurabh/Documents/startup-3/anek-codes/lib/k8s/middleware/pipeline.ts`
- `/Users/sumansaurabh/Documents/startup-3/anek-codes/lib/k8s/devops-agent/session-state.ts`

Kube-agent:

- `/Users/sumansaurabh/Documents/startup-3/kube-agent/pkg/executor/executor.go`
- `/Users/sumansaurabh/Documents/startup-3/kube-agent/pkg/tools/helm.go`

The kube-agent side has the required dispatch handlers:

- `helm_release_uninstall` dispatches to `HelmTools.UninstallRelease`.
- `namespaces_delete` dispatches to `ClusterTools.DeleteNamespace`.

So this specific failure is not caused by a missing kube-agent RPC. The failing responses in the trace are produced before kube-agent executes the destructive action.

## Root cause

There are two interacting problems.

### 1. Natural-language authority is not a server-side confirmation token

The user wrote "I give you full authority", and the LLM treated that as permission to call:

```json
{
  "confirmed": true
}
```

But the server-side confirmation middleware intentionally requires a prior preview token. In `confirmation-tracker.ts`, the protocol is:

1. Tool returns `{ requiresConfirmation: true }`.
2. Middleware stores a Redis token.
3. Later `confirmed: true` is allowed only if that token exists.

Because the first tool call used `confirmed: true` without a prior preview, the middleware returned `CONFIRMATION_REQUIRED`.

This part is expected behavior for server-enforced safety. A prompt-level phrase like "full authority" is not equivalent to a stored confirmation token.

### 2. The failed premature confirmation is recorded as a mutation and blocks the real retry

After the premature `confirmed: true` call fails with `CONFIRMATION_REQUIRED`, `withContextStore` records it in recent mutations.

Then, when the agent correctly previews and retries the same tool with `confirmed: true`, `withConversationDedupe` computes the same mutation fingerprint and returns `CONFLICT`.

This happens because `session-state.ts` intentionally ignores `confirmed` when building mutation fingerprints:

```ts
if (key === 'confirmed' || key === 'outputFormat') return acc
```

That is normally correct because preview and execution should refer to the same effective mutation. The problem is that the context store records a middleware rejection as if it were an attempted mutation.

The key sequence is:

1. `helm_uninstall confirmed=true` is blocked by confirmation middleware.
2. `withContextStore` records the blocked call as a failed recent mutation.
3. `helm_uninstall confirmed=false` returns preview and stores a confirmation token.
4. `helm_uninstall confirmed=true` is now a valid confirmation, but dedupe sees the earlier failed fingerprint.
5. Dedupe returns `CONFLICT` before execution reaches kube-agent.

## Why the user sees "conflict"

The conflict message is not coming from Kubernetes or Helm.

It comes from:

```ts
withConversationDedupe(...)
```

in:

```text
/Users/sumansaurabh/Documents/startup-3/anek-codes/lib/k8s/middleware/pipeline.ts
```

The duplicate guard is treating the actual confirmed execution as a retry of the earlier blocked confirmation attempt.

## Expected behavior

For a destructive command like deleting `metallb-system`, the system should do one of these:

1. If server-side confirmation is mandatory:
   - ignore the user's "full authority" as a direct token,
   - call the tool with `confirmed: false`,
   - surface the preview,
   - wait for explicit user confirmation,
   - call with `confirmed: true`,
   - execute.

2. If the product wants natural-language authority to count as confirmation:
   - convert it into a server-side confirmation token explicitly,
   - do not let the LLM bypass the token protocol by merely setting `confirmed: true`.

The current behavior does neither cleanly: it blocks the premature direct confirmation, but then records that blocked attempt as a recent mutation and prevents the valid second call.

## Recommended fix

Do not record confirmation-gate failures as recent mutations.

In `withContextStore`, skip `recordDevOpsMutation` when the result is a middleware safety rejection, especially:

- `error.code === K8sErrorCode.CONFIRMATION_REQUIRED`
- possibly `error.code === K8sErrorCode.PERMISSION_DENIED`
- possibly `error.code === K8sErrorCode.RATE_LIMITED`

The context store should record only mutations that actually reached the underlying tool or kube-agent, plus maybe real tool-level failures after execution started.

Suggested condition:

```ts
const errorCode =
  typeof r?.error === 'object' && r.error && 'code' in r.error
    ? (r.error as { code?: unknown }).code
    : undefined

const blockedBeforeExecution =
  errorCode === K8sErrorCode.CONFIRMATION_REQUIRED ||
  errorCode === K8sErrorCode.PERMISSION_DENIED ||
  errorCode === K8sErrorCode.RATE_LIMITED

if (isConfirmationPreview || blockedBeforeExecution) {
  return result
}
```

Also consider adding a unit test for this exact sequence:

1. Call destructive tool with `confirmed: true` and no token.
2. Assert `CONFIRMATION_REQUIRED`.
3. Call same tool with `confirmed: false`.
4. Assert `requiresConfirmation: true`.
5. Call same tool with `confirmed: true`.
6. Assert it is not blocked by `CONFLICT`.

## Secondary improvement

The DevOps agent prompt should strongly discourage first-call `confirmed: true` for destructive tools, even when the user says "full authority".

Recommended prompt rule:

```text
For destructive tools, never set confirmed=true unless the same tool call was previewed in this conversation and returned requiresConfirmation=true. A user's natural-language approval means you should produce the preview first, not bypass server confirmation.
```

This does not replace the middleware fix. The server must remain robust when the LLM calls tools in the wrong order.

## Resolution

Implemented in `/Users/sumansaurabh/Documents/startup-3/anek-codes`:

- `lib/k8s/middleware/pipeline.ts` now skips mutation recording for pre-execution middleware blocks:
  - `CONFIRMATION_REQUIRED`
  - `PERMISSION_DENIED`
  - `RATE_LIMITED`
- `lib/k8s/devops-agent/system-prompt.ts` now tells the DevOps agent not to set `confirmed=true` for destructive tools until the same effective tool call has already produced a confirmation preview.

Validation:

- `pnpm exec eslint lib/k8s/middleware/pipeline.ts lib/k8s/devops-agent/system-prompt.ts`
- `pnpm exec tsc --noEmit --pretty false`

## Kube-agent impact

No kube-agent code change appears necessary for this specific issue.

The Go kube-agent already supports:

- `helm_release_uninstall`
- `namespaces_delete`

The operation is being blocked in the Anek TypeScript middleware before the Go agent receives the final mutation request.
