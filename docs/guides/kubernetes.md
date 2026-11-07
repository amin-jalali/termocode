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
| `F` | Pod: browse its files; pick a file to download it (asks for a local path). |
| `U` | Pod: upload a local file into it (asks the paths, then asks first). |
| `D` | Pod: open a shell in a new debug container (asks first). |
| `y` | Copy the name. On a **FORWARDS** row: copy the local address. |
| `!` | Your custom commands for the row (pods, deployments, services, jobs; see the Containers guide, "Custom commands"). Each asks first. |
| `r` | Deployment: restart it (asks first). |
| `s` | Deployment or statefulset: scale it (asks the number, then asks first). |
| `o` | Deployment: rollout status and history; roll back to an older revision (asks first). |
| `I` | Deployment: set the image of one container (asks the image, then asks first). |
| `d` | Delete the row's object (asks first). Not on a node or an event. |
| `:` | Show any kind as one more section: a CRD, a short name, an alias. `" -A"` at the end: every namespace. |
| `A` | The `:` kind in every namespace, or in the view's one again. |
| `O` | Edit the row's YAML in a file (not Secrets). Save it, then press `a`. |
| `a` | Apply the editor's saved file: shows the diff, then asks first. |
| `x` | Cluster report of the namespace (a read-only page). |
| `P` | Permissions of the namespace (a read-only page). |
| `N` | Nodes of the cluster: cordon, uncordon or drain one (asks first). |
| `S` | Autoscalers (HPA) of the namespace (a read-only page). |
| `c` / `n` | Pick the context / the namespace. |
| `[` / `]` | Back / forward to the context and namespace you had before. |
| `t` | Metrics: CPU and memory of the nodes and the pods (a read-only page). |
| `H` | Helm releases of the namespace: status, values, history, roll back, uninstall. |
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

## Pod files and debug

`F` on a pod lists its root folder (`kubectl exec … ls -la`; read only).
Pick a folder to open it, or `..` to go up. Pick a file to download it:
termocode asks for a local path (the workspace folder first) and runs
`kubectl cp`. It never overwrites a file that is there.

`U` on a pod uploads a file: it asks for the local file (the current
file first), then for the path in the pod (`/tmp/<name>` first). A card
shows both paths, the context, the namespace and the command.
**Cancel** is the first button and has the focus. A file at that pod
path is replaced.

`D` on a pod adds an ephemeral debug container (`busybox`) and opens a
shell in it, in a terminal tab (`kubectl debug -it`). It shares the
processes of the pod's first container. This changes the pod spec, so a
card asks first (**Cancel** has the focus). The debug container stays
until the pod is gone.

These use the pod's default container. The AI can not run them.

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

## Metrics

When the cluster runs the metrics-server, each load also reads
`kubectl top pods`. A wide pod row shows its CPU and memory (`12m
64Mi`). The footer of a pod shows them too, with a sparkline of the
last samples (5 seconds apart).

`t` (or **Kubernetes: Show Metrics** in the palette) opens a read-only
page: CPU and memory of every node (with the share of the node, `25%`)
and of every pod in the namespace. A node with no metrics yet says
`unknown`.

A cluster without the metrics-server is asked once; the rows then show
no metrics. `R` asks again.

## Restart a deployment

`r` on a deployment asks first. The card shows the deployment, the
context, the namespace and the command (`kubectl rollout restart`).
**Cancel** is the first button and has the focus, so `Enter` or `Esc`
alone never restarts anything. **Restart** replaces the pods one by one.

## Scale a deployment or a statefulset

`s` on a deployment or a statefulset asks for the number of replicas; it starts at the
current number. A number from 0 to 1000 opens a card with the change
(`2 → 5`), the context, the namespace and the command (`kubectl
scale`). 0 stops every pod, and the card says so. **Cancel** is the
first button and has the focus, so `Enter` or `Esc` alone never scales
anything. The same number as now changes nothing.

## Rollout: status, history, roll back

`o` on a deployment reads `kubectl rollout status` and `kubectl rollout
history` and opens a list. Nothing changes yet.

- **Status and history** opens a read-only page with the status and
  every revision, newest first, with its change cause.
- The newest revision says **current**.
- An older revision rolls the deployment back to it. A card shows the
  deployment, the revision, the context, the namespace and the command
  (`kubectl rollout undo --to-revision`). **Cancel** is the first button
  and has the focus, so `Enter` or `Esc` alone never rolls anything back.

## Set image

`I` on a deployment asks for the new image as `container=image`. It
starts with the first container and its image now; the note lists every
container. With one container you can type only the image. A card then
shows the change (`shop/api:v3 → shop/api:v4`), the context, the
namespace and the command (`kubectl set image`). **Cancel** is the first
button and has the focus. The same image as now changes nothing.

## Delete an object

`d` on a pod, deployment, service or job asks first. The card shows the
object, the context, the namespace and the command (`kubectl delete`).
**Cancel** is the first button and has the focus, so `Enter` or `Esc`
alone never deletes anything. **Delete** does not wait: the list shows
the object as it goes. A pod that belongs to a deployment or a job is
made again; deleting a deployment or a job deletes its pods too.

## Any kind

`:` (or **Kubernetes: Show Kind…** in the palette) reads `kubectl
api-resources`, so it knows every kind of the cluster, the custom ones
(CRDs) too. Type a kind:

- its plural, singular or short name (`certificates`, `certificate`,
  `cert`), its Kind (`Certificate`), or `name.group`
  (`certificates.cert-manager.io`) when two groups use the same name;
- or an alias of your own (see below).

`Tab` completes the name. Add ` -A` to list every namespace. The kind
shows as one more section after **JOBS** and loads with the others.
`i`, `v`, `E` and `y` work on its rows, in the object's own namespace;
the rows change nothing. `A` switches between every namespace and the
view's one. `:` with an empty answer hides the section.

### Aliases

Put your own short names in `kube_aliases.json`, next to `config.json`
(`~/.config/termocode/`):

```json
{"tls": "certificates.cert-manager.io", "wg": "widgets"}
```

The `:` prompt lists them.

## Helm releases

`H` (or **Kubernetes: Helm Releases…** in the palette) lists the helm
releases of the view's context and namespace: the name, the chart, the
status and the revision. You need `helm` on your `PATH`; without it the
view says so and does nothing else.

A release opens a short list:

- **Status**, **Values** and **History** open read-only pages. Values
  that look secret (passwords, tokens, keys) are masked.
- **Roll Back…** lists the older revisions. A card shows the release,
  the revision, the context, the namespace and the command (`helm
  rollback --wait`).
- **Uninstall…** asks first. The card shows the release and the command
  (`helm uninstall`); it deletes every object of the chart.

On both cards **Cancel** is the first button and has the focus, so
`Enter` or `Esc` alone never changes anything.

## Cluster report

**Kubernetes: Cluster Report** (or `x` in the view) opens a read-only page
about the namespace:

- **Pulse**: for pods, deployments, services and jobs, how many are ok,
  how many warn and how many fail.
- **Xray**: the owner tree. Each deployment, service and job with the pods
  it owns or selects, and their status.
- **Report**: the problems found, for example crashing or restarting
  pods, pods with no limits or no readiness probe, images with no fixed
  tag, services that select no pod, failed jobs.

It only reads the cluster. The AI may open it.

## Permissions (RBAC)

**Kubernetes: Permissions (RBAC)** (or `P` in the view) opens a read-only
page about the namespace:

- **What you may do**: each resource and its verbs, from
  `kubectl auth can-i --list`. It asks the cluster about you; nothing
  changes.
- **Role bindings**: each binding, its role and its subjects (users,
  groups, service accounts).
- **Service accounts** of the namespace.

When you may not read one part, that part says why and the others still
show. The AI may open the page.

## Nodes and autoscalers

**Kubernetes: Nodes…** (or `N` in the view) lists the nodes of the
cluster with their status, roles, version and age. Pick a node to see
what you can do:

- **Cordon**: no new pods go to the node. Its running pods stay.
- **Uncordon** (on a cordoned node): it takes new pods again.
- **Drain**: cordon the node and evict its pods, so they start on other
  nodes. DaemonSet pods stay; `emptyDir` data of evicted pods is lost.

Each one asks first. The card shows the node, the context and the
command. **Cancel** is the first button and has the focus, so `Enter` or
`Esc` alone never changes a node.

**Kubernetes: Autoscalers (HPA)** (or `S` in the view) opens a read-only
page of the namespace's horizontal pod autoscalers: the target, min and
max replicas, how many run now and how many it wants, and the metrics.

## Edit and apply YAML

**Kubernetes: Apply Current File…** (or `a` in the view) applies the file
of the editor tab to the cluster. Save it first. termocode runs
`kubectl diff -f` (a server-side dry-run: nothing changes) and opens the
diff as a read-only page. Then a card shows the file, the context, the
namespace, the number of changed lines and the command (`kubectl
apply -f`). **Cancel** is the first button and has the focus, so `Enter`
or `Esc` alone never applies anything. When the cluster already matches
the file, nothing is asked.

`O` on a row writes the object's YAML to a private temp file and opens it
in the editor. Change it, save it, and apply it as above. Secrets are not
opened this way: their values are masked, so the copy would break them.

## New manifest from a snippet

**Kubernetes: New Manifest from Snippet…** opens a new YAML buffer with a
Deployment, Service, Ingress, ConfigMap, Secret, Job, CronJob,
PersistentVolumeClaim or StatefulSet. The name is `my-app` and the
namespace is the view's namespace; change them and save the file. Nothing
is sent to the cluster.

## Kustomize preview

**Kubernetes: Kustomize Preview** builds the folder of the current file
with `kubectl kustomize` and shows the result in a read-only YAML page.
Secret values are masked. Nothing is sent to the cluster.

## Helm repos and charts

**Kubernetes: Add Helm Repo…** asks for a name and a URL
(`bitnami https://charts.bitnami.com/bitnami`). A card shows the command
(`helm repo add`); **Cancel** is the first button and has the focus. It
changes your helm repo list, not the cluster.

**Kubernetes: Install Helm Chart…** asks for a release name, a chart and
an optional version (`web bitnami/nginx 15.1.0`). The chart goes to the
view's context and namespace. A card shows the scope and the command
(`helm install`); **Cancel** has the focus. helm's notes open in a
read-only page. You need `helm` on your `PATH`.

## The AI

The AI can open the view, load it again and open the metrics page. It can also read the
cluster, in the context the view shows: `kube_list` (the four lists),
`kube_describe` (one object) and `kube_logs` (the last lines of a pod).
Secrets in their answers are hidden. The AI can never restart,
scale, roll back or change the image of a deployment, delete an
object, apply a file, cordon or drain a node, start a port-forward,
copy pod files, debug a pod, use helm, add a helm repo, install a
chart or switch the context.

## When something is wrong

| The view says | What to do |
|---|---|
| kubectl not found | Install `kubectl`, then press `R`. |
| No cluster set up | Your kubeconfig has no current context. Set one up with your cluster's tools, or press `c`. |
| Cannot reach the cluster | The cluster or your network is down. `R` tries again; `c` picks another context. |
| Not allowed: … | Your user may not list this kind in this namespace. The other sections still show. |
