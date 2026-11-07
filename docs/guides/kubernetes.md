# Kubernetes

The **Kubernetes** view shows one cluster: the pods, deployments,
services and jobs of one namespace. It reads the cluster with your own
`kubectl`, so it sees what `kubectl` sees. It is read-only, with two
exceptions: you can restart or scale a deployment and delete one
object, each after a question.

## Open the view

- Run **Kubernetes: Show** from the command palette (`F1`).

The view lives under the Containers icon in the activity bar. A click on
that icon goes back to the Containers view.

You need `kubectl` on your `PATH` and a kubeconfig with a current
context. termocode uses the same kubeconfig as `kubectl`: `KUBECONFIG`,
or `~/.kube/config`.

## What you see

The line under the title shows the context and the namespace:
`ctx dev · ns shop`. The view starts with the kubeconfig's current
context and that context's namespace (`default` when it names none).

| Section | A row shows |
|---|---|
| **PODS** | ready containers (`2/2`), restarts (`↻7`) and age. A wide view also shows the status (`Running`, `CrashLoopBackOff`, …). |
| **DEPLOYMENTS** | ready pods (`1/2`) and age. A wide view also shows updated and available pods. |
| **SERVICES** | the type (`ClusterIP`, `NodePort`, …). A wide view also shows the ports. |
| **JOBS** | finished runs (`2/5`) and age. |
| **EVENTS** | the namespace's newest 50 events: the object (`pod/api-1`), the reason (`BackOff`) and when it last happened. A warning has a red mark. The footer shows the message. `i`, `v` and `E` on an event act on its object. |
| **STATEFULSETS** | ready pods (`1/2`) and age. |
| **DAEMONSETS** | ready pods of the wanted ones (`3/3`) and age. A wide view also shows updated and available pods. |
| **REPLICASETS** | ready pods (`2/2`) and age. The footer shows the owner (`Deployment/api`). |
| **CRONJOBS** | the schedule (`*/5 * * * *`, `(off)` when suspended) and age. The footer shows when it last ran. |
| **CONFIGMAPS** | how many keys it has. The footer lists the key names. |
| **SECRETS** | how many keys it has; a wide view also shows the type. The footer lists the key names. The values are never read. |
| **INGRESSES** | the hosts (`shop.example.com`, `*` for any host). A wide view also shows the ports. |
| **PVCS** | the status (`Bound`, `Pending`) and the size. A wide view also shows the access modes. |
| **NODES** | the status (`Ready`, `NotReady`, `SchedulingDisabled`). A wide view also shows the version. Nodes belong to the whole cluster, not to the namespace. |
| **FORWARDS** | only while a port-forward runs: each forward (`svc/web`), its local port and the remote port (`:43123 → 80`). |

The sections from **EVENTS** to **NODES** start folded. A folded
section does not load; open it (`Space`, `Enter` or a click) and it
loads now and then with the others. Fold it again and it stops loading.

The mark at the start of a row is green when all is well, yellow while
it is on its way, red when it is broken and grey when it is done.

The footer shows more about the row under the cursor, and the keys for it.

The lists load again every 5 seconds, but only while the view is on
screen and the window is in front. When the cluster does not answer, the
view says so in one line and waits longer before it tries again.

## Context and namespace

- `c` picks another context.
- `n` picks another namespace.

The choice is for this termocode window only. termocode passes it to
`kubectl` as `--context` and `--namespace`. It never changes your
kubeconfig, so other terminals keep their context.

## Keys

| Key | What it does |
|---|---|
| `↑` `↓` `j` `k` | Move. `g` / `G` go to the top / the end. |
| `Tab` | Next section. |
| `Space` | Fold or open a section. |
| `/` | Filter the rows by name. `Enter` keeps the filter, `Esc` clears it. |
| `Enter` / `l` | Pod: follow its logs. Other rows: describe. |
| `i` | Describe the row (a read-only page). |
| `v` | Show the row as YAML (a read-only page; Secret values are masked). |
| `E` | Events of the row, newest first (a read-only page). On a section header: all events of the namespace. |
| `L` | Pod: pick how to show the logs (previous container, last 5 minutes or hour, time stamps, save to a file). Deployment: the logs of all its pods. |
| `e` | Pod: open a shell in it. |
| `f` | Pod, service or deployment: start or stop a port-forward. On a **FORWARDS** row: stop it. |
| `y` | Copy the name. On a **FORWARDS** row: copy the local address. |
| `r` | Deployment: restart it (asks first). |
| `s` | Deployment: scale it (asks the number, then asks first). |
| `d` | Delete the row's object (asks first). Not on a node or an event. |
| `c` / `n` | Pick the context / the namespace. |
| `[` / `]` | Back / forward to the context and namespace you had before. |
| `R` | Load the lists now. |
| `w` | Save the screen as a text file (asks for the name; never overwrites a file). |
| `?` | Show these keys. |
| `Esc` | Back to the editor. |

## Logs and shell

`Enter` (or `l`) on a pod follows its logs in a terminal tab, from the
last 200 lines. `e` opens a shell in the pod (`bash`, or `sh` when there
is no bash). A pod with more than one container asks which container
first.

`L` on a pod picks how to show its logs:

- **Follow logs**: as `Enter`.
- **Previous container**: the logs of the container before its last
  restart (useful for a crashing pod). They do not follow.
- **Last 5 minutes** / **Last hour**: only the newer lines, then new ones.
- **With time stamps**: a time before each line.
- **Save to a file…**: every line, once. termocode asks the file name
  (in the workspace) and never overwrites a file.

`L` on a deployment follows the logs of all its pods and containers in
one tab; each line starts with its pod and container.

## Port-forward

`f` on a pod, a service or a deployment forwards one of its ports to
your machine: a pod's container ports, a service's ports, or the
container ports of a deployment's pod template. For a service or a
deployment, `kubectl` picks one of its pods. `kubectl` picks a free local
port; the row and the footer show it (`127.0.0.1:43123 → 8080`). When
there is more than one port, termocode asks which one first. `f` again
stops the forward.

While a forward runs, the **FORWARDS** section at the end lists every
active forward, also the ones of another context or namespace. `f` on a
row there stops it; `y` copies its local address (`127.0.0.1:43123`).
Every forward stops when you quit termocode.

## Restart a deployment

`r` on a deployment asks first. The card shows the deployment, the
context, the namespace and the command (`kubectl rollout restart`).
**Cancel** is the first button and has the focus, so `Enter` or `Esc`
alone never restarts anything. **Restart** replaces the pods one by one.

## Scale a deployment

`s` on a deployment asks for the number of replicas; it starts at the
current number. A number from 0 to 1000 opens a card with the change
(`2 → 5`), the context, the namespace and the command (`kubectl
scale`). 0 stops every pod, and the card says so. **Cancel** is the
first button and has the focus, so `Enter` or `Esc` alone never scales
anything. The same number as now changes nothing.

## Delete an object

`d` on a pod, deployment, service or job asks first. The card shows the
object, the context, the namespace and the command (`kubectl delete`).
**Cancel** is the first button and has the focus, so `Enter` or `Esc`
alone never deletes anything. **Delete** does not wait: the list shows
the object as it goes. A pod that belongs to a deployment or a job is
made again; deleting a deployment or a job deletes its pods too.

## New manifest from a snippet

**Kubernetes: New Manifest from Snippet…** opens a new YAML buffer with a
Deployment, Service, Ingress, ConfigMap, Secret, Job, CronJob,
PersistentVolumeClaim or StatefulSet. The name is `my-app` and the
namespace is the view's namespace; change them and save the file. Nothing
is sent to the cluster.

## The AI

The AI can open the view and load it again. It can also read the
cluster, in the context the view shows: `kube_list` (the four lists),
`kube_describe` (one object) and `kube_logs` (the last lines of a pod).
Secrets in their answers are hidden. The AI can never restart or
scale a deployment, delete an object, start a port-forward or switch
the context.

## When something is wrong

| The view says | What to do |
|---|---|
| kubectl not found | Install `kubectl`, then press `R`. |
| No cluster set up | Your kubeconfig has no current context. Set one up with your cluster's tools, or press `c`. |
| Cannot reach the cluster | The cluster or your network is down. `R` tries again; `c` picks another context. |
| Not allowed: … | Your user may not list this kind in this namespace. The other sections still show. |
