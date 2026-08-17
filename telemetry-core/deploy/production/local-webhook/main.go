package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"
)

type Alert struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
}

type AlertmanagerPayload struct {
	Version           string            `json:"version"`
	GroupKey          string            `json:"groupKey"`
	TruncatedAlerts   int               `json:"truncatedAlerts"`
	Status            string            `json:"status"`
	Receiver          string            `json:"receiver"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
	Alerts            []Alert           `json:"alerts"`
}

// sendNotification displays a desktop notification with the specified title and message on supported operating systems.
func sendNotification(title, message string) {
	switch runtime.GOOS {
	case "darwin":
		// macOS notification
		script := fmt.Sprintf(`display notification "%s" with title "%s" sound name "Glass"`, message, title)
		exec.Command("osascript", "-e", script).Run()
	case "linux":
		// Linux notification (requires notify-send)
		exec.Command("notify-send", "-u", "critical", title, message).Run()
	case "windows":
		// Windows notification via PowerShell
		script := fmt.Sprintf(`[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null; $template = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02); $template.SelectSingleNode('//text[@id="1"]').InnerText = '%s'; $template.SelectSingleNode('//text[@id="2"]').InnerText = '%s'`, title, message)
		exec.Command("powershell", "-Command", script).Run()
	}
}

// webhookHandler processes Alertmanager webhook POST requests and sends a desktop notification for each alert. It responds with the received status after successful processing.
func webhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload AlertmanagerPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Printf("Error decoding payload: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	log.Printf("Received alert: status=%s, receiver=%s, alerts=%d",
		payload.Status, payload.Receiver, len(payload.Alerts))

	for _, alert := range payload.Alerts {
		title := fmt.Sprintf("[%s] %s", alert.Status, alert.Labels["alertname"])
		message := alert.Annotations["summary"]
		if message == "" {
			message = alert.Annotations["description"]
		}

		log.Printf("Alert: %s - %s (severity: %s)",
			alert.Labels["alertname"],
			message,
			alert.Labels["severity"])

		// Send desktop notification
		sendNotification(title, message)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "received"})
}

// criticalHandler handles critical alert webhook requests.
func criticalHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("CRITICAL alert received!")
	webhookHandler(w, r)
}

// warningHandler logs and processes warning alert webhook requests.
func warningHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("WARNING alert received")
	webhookHandler(w, r)
}

// localHandler processes alerts received for the local system endpoint.
func localHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("LOCAL SYSTEM alert received")
	webhookHandler(w, r)
}

// healthHandler reports that the local webhook service is healthy.
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

// main starts the alert webhook HTTP server using the port specified by the PORT environment variable or port 9095 by default.
func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "9095"
	}

	http.HandleFunc("/webhook", webhookHandler)
	http.HandleFunc("/webhook/critical", criticalHandler)
	http.HandleFunc("/webhook/warning", warningHandler)
	http.HandleFunc("/webhook/local", localHandler)
	http.HandleFunc("/health", healthHandler)

	log.Printf("Starting alert webhook receiver on port %s", port)
	log.Printf("Endpoints:")
	log.Printf("  POST /webhook         - Default alerts")
	log.Printf("  POST /webhook/critical - Critical alerts")
	log.Printf("  POST /webhook/warning  - Warning alerts")
	log.Printf("  POST /webhook/local    - Local system alerts")
	log.Printf("  GET  /health          - Health check")

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
