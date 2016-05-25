package translators

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"

	"github.com/prometheus/alertmanager/notify"
)

// victoropsPost - The VictorOps REST Endpoint accepts alerts from any source
// via an HTTPS POST request. Only the message_type is required.
// http://victorops.force.com/knowledgebase/articles/Integration/Alert-Ingestion-API-Documentation/
type victoropsPost struct {

	// MessageType - One of the following values: INFO, WARNING, ACKNOWLEDGEMENT,
	// CRITICAL, RECOVERY
	//
	// CRITICAL messages raise incidents in VictorOps, you can also configure
	// your settings to raise incidents for WARNING messages.
	// INFO messages become part of your timeline but do not raise incidents.
	MessageType string `json:"message_type"`

	// EntityID - The name of alerting entity. If not provided, VictorOps
	// will assign a random name. VictorOps uses the entity_id field to identify
	// the monitored entity (host, service, metric, etc.)
	EntityID string `json:"entity_id,omitempty"`

	// Timestamp - Timestamp of the alert in seconds since epoch. Defaults to the
	// time the alert is received at VictorOps.
	Timestamp int64 `json:"timestamp,omitempty"`

	// StateStartTime - The time this entity entered its current state
	// (seconds since epoch). Defaults to the time alert is received.
	StateStartTime int64 `json:"state_start_time,omitempty"`

	// StateMessage - Any additional status information from the alert item.
	StateMessage string `json:"state_message,omitempty"`

	// MonitoringTool - The name of the monitoring system software
	// (eg. nagios, icinga, sensu, etc.)
	MonitoringTool string `json:"monitoring_tool,omitempty"`

	// EntityDisplayName - Used within VictorOps to display a human-readable
	// name for the entity.
	EntityDisplayName string `json:"entity_display_name,omitempty"`

	// AckMessage - A user entered comment for the acknowledgment.
	AckMessage string `json:"ack_msg,omitempty"`

	// AckAuthor - The user that acknowledged the incident.
	AckAuthor string `json:"ack_author,omitempty"`
}

// victoropsResponse - The HTTP result code will indicate success or failure,
// with the following JSON values in the response body
type victoropsResponse struct {

	// Result - "success" or "failure"
	Result string `json:"result"`

	// EntityID - The id passed in with the POST request, or the id randomly
	// assigned by VictorOps. You should continue to pass us this id for
	// subsequent alerts that pertain to the same incident.
	EntityID string `json:"entity_id"`

	// Message - Error message (if any)
	Message string `json:"message,omitempty"`
}

func init() {
	Handles["/victorops"] = victorops
}

const (
	victoropsApikeyParam     = "api_key"
	victoropsRoutingkeyParam = "routing_key"
)

// victorops(rw http.ResponseWriter, req *http.Request)
// the POST handle for Alertmanager translation to VictorOps
func victorops(rw http.ResponseWriter, req *http.Request) {
	log.Debugf("Recieved VictorOps translation request: %s", req.URL)
	// Attempt decode
	decoder := json.NewDecoder(req.Body)
	var wm notify.WebhookMessage
	err := decoder.Decode(&wm)
	if err != nil {
		log.Errorf("Could not decode Alertmanager request body: %s", err.Error())
		rw.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(rw, "Could not decode Alertmanager request body: %s", err.Error())
		return
	} else if wm.Data == nil {
		log.Errorf("Missing fields in request body")
		rw.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(rw, "Missing fields request body")
		return
	}
	// Extract query params
	apiKey := ""
	routingKey := ""
	if val, ok := req.URL.Query()[victoropsApikeyParam]; ok && len(val) > 0 {
		apiKey = val[0]
	}
	if val, ok := req.URL.Query()[victoropsRoutingkeyParam]; ok && len(val) > 0 {
		routingKey = val[0]
	}

	// Validate query params
	if apiKey == "" || routingKey == "" {
		log.Errorf("Missing request query parameters")
		rw.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(rw, "requires query parameters '%s' and '%s'", victoropsApikeyParam, victoropsRoutingkeyParam)
		return
	}

	// Translate Alertmanager WebhookMessage to victoropsPost
	status := wm.Status
	groupKey := wm.GroupKey
	displayName := strings.Join(wm.GroupLabels.Values(), ":")

	if status == "firing" {
		// Create an alert for each issue in the group
		for _, alert := range wm.Alerts {
			messageType := "CRITICAL"
			// extract victorops_message_type label if defined
			if alert.Labels["victorops_message_type"] != "" {
				messageType = alert.Labels["victorops_message_type"]
			}
			// combine all annotations, alerts, and urls into state message
			stateMessage := ""
			for k, v := range alert.Annotations {
				stateMessage += k + ": " + v + "\n"
			}
			for k, v := range alert.Labels {
				stateMessage += k + ": " + v + "\n"
			}
			stateMessage += "Prometheus: " + alert.GeneratorURL + "\n"
			stateMessage += "Alertmanager: " + wm.ExternalURL

			// build alert
			vp := victoropsPost{
				MessageType:       messageType,
				EntityID:          strconv.FormatUint(groupKey, 10),
				Timestamp:         time.Now().Unix(),
				StateStartTime:    alert.StartsAt.Unix(),
				StateMessage:      stateMessage,
				MonitoringTool:    "Prometheus Alertmanager",
				EntityDisplayName: displayName,
			}
			// marshall and send alert
			b, err := json.Marshal(vp)
			if err == nil {
				// Post Alert
				resp, err := http.Post("https://alert.victorops.com/integrations/generic/20131114/alert/"+apiKey+"/"+routingKey, "application/json", bytes.NewBuffer(b))
				//resp, err := http.Post("http://localhost:8080/repeat?api="+apiKey+"&route="+routingKey, "application/json", bytes.NewBuffer(b))
				if err != nil {
					log.Errorf("Failed post to VictorOps REST api: %s", err.Error())
					rw.WriteHeader(http.StatusBadGateway)
					fmt.Fprintf(rw, "Failed post to VictorOps REST api: %s", err.Error())
					return
				}

				// Decode response
				decoder := json.NewDecoder(resp.Body)
				var vr victoropsResponse
				err = decoder.Decode(&vr)
				if err != nil {
					log.Errorf("Could not decode VictorOps response body: %s", err.Error())
					//rw.WriteHeader(http.StatusBadGateway)
					fmt.Fprintf(rw, "Could not decode VictorOps response body: %s", err.Error())
				}

				// Check Response
				if resp.StatusCode/100 != 2 {
					log.Errorf("Unexpected status code %v from VictorOps: %v", resp.StatusCode, vr.Message)
					rw.WriteHeader(http.StatusBadGateway)
					fmt.Fprintf(rw, "Unexpected status code %v from VictorOps: %v", resp.StatusCode, vr.Message)
					return
				}

			} else {
				// failed to marshall alert
				log.Errorf("Failed to marshall victoropsPost: %s", err.Error())
				rw.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintf(rw, "Failed to marshall victoropsPost: %s", err.Error())
				return
			}
		}
	} else if status == "resolved" {
		vp := victoropsPost{
			MessageType:       "RECOVERY",
			EntityID:          strconv.FormatUint(groupKey, 10),
			Timestamp:         time.Now().Unix(),
			StateMessage:      "Entity recovered",
			MonitoringTool:    "Prometheus Alertmanager",
			EntityDisplayName: displayName,
		}
		// marshall and send alert
		b, err := json.Marshal(vp)
		if err == nil {
			// Post Alert
			resp, err := http.Post("https://alert.victorops.com/integrations/generic/20131114/alert/"+apiKey+"/"+routingKey, "application/json", bytes.NewBuffer(b))
			//resp, err := http.Post("http://localhost:8080/repeat?api="+apiKey+"&route="+routingKey, "application/json", bytes.NewBuffer(b))
			if err != nil {
				log.Errorf("Failed post to VictorOps REST api: %s", err.Error())
				rw.WriteHeader(http.StatusBadGateway)
				fmt.Fprintf(rw, "Failed post to VictorOps REST api: %s", err.Error())
				return
			}

			// Decode response
			decoder := json.NewDecoder(resp.Body)
			var vr victoropsResponse
			err = decoder.Decode(&vr)
			if err != nil {
				log.Errorf("Could not decode VictorOps response body: %s", err.Error())
				rw.WriteHeader(http.StatusBadGateway)
				fmt.Fprintf(rw, "Could not decode VictorOps response body: %s", err.Error())
				return
			}

			// Check Response
			if resp.StatusCode/100 != 2 {
				log.Errorf("Unexpected status code %v from VictorOps: ", vr.Message)
				rw.WriteHeader(http.StatusBadGateway)
				fmt.Fprintf(rw, "Unexpected status code %v from VictorOps: ", vr.Message)
				return
			}

		} else {
			// failed to marshall alert
			log.Errorf("Failed to marshall victoropsPost: %s", err.Error())
			rw.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(rw, "Failed to marshall victoropsPost: %s", err.Error())
			return
		}
	} else {
		log.Errorf("Unknown Alertmanager status: %s", status)
		rw.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(rw, "Unknown Alertmanager status: %s", status)
		return
	}
}
