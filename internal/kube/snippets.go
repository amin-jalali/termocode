package kube

import "strings"

// Snippet is a manifest template (CTX-39). Body uses {{name}} and
// {{namespace}}; a line holding only "namespace: {{namespace}}" is dropped
// when no namespace is given.
type Snippet struct {
	ID, Title, Kind string
	Body            string
}

var snippets = []Snippet{
	{ID: "deployment", Title: "Deployment", Kind: "Deployment", Body: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{name}}
  namespace: {{namespace}}
  labels:
    app: {{name}}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: {{name}}
  template:
    metadata:
      labels:
        app: {{name}}
    spec:
      containers:
        - name: {{name}}
          image: nginx:1.27
          ports:
            - containerPort: 80
          resources:
            requests:
              cpu: 100m
              memory: 128Mi
            limits:
              memory: 256Mi
`},
	{ID: "service", Title: "Service", Kind: "Service", Body: `apiVersion: v1
kind: Service
metadata:
  name: {{name}}
  namespace: {{namespace}}
spec:
  selector:
    app: {{name}}
  ports:
    - port: 80
      targetPort: 80
`},
	{ID: "ingress", Title: "Ingress", Kind: "Ingress", Body: `apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: {{name}}
  namespace: {{namespace}}
spec:
  rules:
    - host: {{name}}.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: {{name}}
                port:
                  number: 80
`},
	{ID: "configmap", Title: "ConfigMap", Kind: "ConfigMap", Body: `apiVersion: v1
kind: ConfigMap
metadata:
  name: {{name}}
  namespace: {{namespace}}
data:
  KEY: value
`},
	{ID: "secret", Title: "Secret (values to fill in)", Kind: "Secret", Body: `apiVersion: v1
kind: Secret
metadata:
  name: {{name}}
  namespace: {{namespace}}
type: Opaque
stringData:
  KEY: ""
`},
	{ID: "job", Title: "Job", Kind: "Job", Body: `apiVersion: batch/v1
kind: Job
metadata:
  name: {{name}}
  namespace: {{namespace}}
spec:
  backoffLimit: 2
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: {{name}}
          image: busybox:1.36
          command: ["sh", "-c", "echo hello"]
`},
	{ID: "cronjob", Title: "CronJob", Kind: "CronJob", Body: `apiVersion: batch/v1
kind: CronJob
metadata:
  name: {{name}}
  namespace: {{namespace}}
spec:
  schedule: "0 3 * * *"
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: OnFailure
          containers:
            - name: {{name}}
              image: busybox:1.36
              command: ["sh", "-c", "echo hello"]
`},
	{ID: "pvc", Title: "PersistentVolumeClaim", Kind: "PersistentVolumeClaim", Body: `apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: {{name}}
  namespace: {{namespace}}
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
`},
	{ID: "statefulset", Title: "StatefulSet", Kind: "StatefulSet", Body: `apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: {{name}}
  namespace: {{namespace}}
spec:
  serviceName: {{name}}
  replicas: 1
  selector:
    matchLabels:
      app: {{name}}
  template:
    metadata:
      labels:
        app: {{name}}
    spec:
      containers:
        - name: {{name}}
          image: nginx:1.27
          volumeMounts:
            - name: data
              mountPath: /data
  volumeClaimTemplates:
    - metadata:
        name: data
      spec:
        accessModes: ["ReadWriteOnce"]
        resources:
          requests:
            storage: 1Gi
`},
}

// Snippets lists the manifest snippets.
func Snippets() []Snippet { return append([]Snippet(nil), snippets...) }

// Render fills a snippet in. name is made a DNS label ("" = my-app).
func Render(id, name, namespace string) (string, bool) {
	for _, s := range snippets {
		if s.ID != id {
			continue
		}
		name = dnsLabel(name)
		namespace = strings.TrimSpace(namespace)
		if namespace != "" {
			namespace = dnsLabel(namespace)
		}
		var out []string
		for _, l := range strings.Split(s.Body, "\n") {
			if strings.TrimSpace(l) == "namespace: {{namespace}}" && namespace == "" {
				continue
			}
			l = strings.ReplaceAll(l, "{{name}}", name)
			l = strings.ReplaceAll(l, "{{namespace}}", namespace)
			out = append(out, l)
		}
		return strings.Join(out, "\n"), true
	}
	return "", false
}

func dnsLabel(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 63 {
		out = strings.Trim(out[:63], "-")
	}
	if out == "" {
		return "my-app"
	}
	return out
}
