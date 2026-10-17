// Package services finds and drives a project's dev services: Docker
// Compose services (compose.yaml) and local commands
// (.termocode/services.json, Procfile).
//
// Rules:
//   - On demand only. Status is read when the Services view asks
//     (`docker compose ps --all --format json`); there is no timer. While
//     the view is visible, one `docker events` stream (Watch) tells the app
//     when to read again; it is cancelled when the view hides, so a hidden
//     view costs no process and no goroutine.
//   - Every child process starts in exec.go through internal/spawn, in its
//     own process group (a cancel kills docker and its children).
//   - Long or visible work (up, logs -f, exec) is returned as a Command the
//     app runs in a terminal tab; short work (ps, config, stop, restart,
//     logs --tail) is captured with a timeout.
//   - Env values never leave the package: Service.EnvKeys holds names only,
//     and secret-looking values are handed to the AI redactor.
package services
