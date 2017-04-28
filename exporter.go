package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"
)

type NginxStats struct {
	Connections struct {
		Accepted int `json:"accepted"`
		Dropped  int `json:"dropped"`
		Active   int `json:"active"`
		Idle     int `json:"idle"`
	} `json:"connections"`
	SSLs struct {
		Handshakes       int64 `json:"handshakes"`
		HandshakesFailed int64 `json:"handshakes_failed"`
		SessionReuses    int64 `json:"session_reuses"`
	} `json:"ssl"`
	Requests struct {
		Total   int64 `json:"total"`
		Current int   `json:"current"`
	} `json:"requests"`

	ServerZones   map[string]Server   `json:"server_zones"`
	UpstreamZones map[string]Upstream `json:"upstreams"`
}

type Server struct {
	Processing int   `json:"processing"`
	Requests   int64 `json:"requests"`
	Responses  struct {
		Responses1xx int64 `json:"1xx"`
		Responses2xx int64 `json:"2xx"`
		Responses3xx int64 `json:"3xx"`
		Responses4xx int64 `json:"4xx"`
		Responses5xx int64 `json:"5xx"`
		Total        int64 `json:"total"`
	} `json:"responses"`
	Discarded int64 `json:"discarded"` // added in version 6
	Received  int64 `json:"received"`
	Sent      int64 `json:"sent"`
}

type Upstream struct {
	Peers []struct {
		ID        *int   `json:"id"`
		Server    string `json:"server"`
		Backup    bool   `json:"backup"`
		Weight    int    `json:"weight"`
		State     string `json:"state"`
		Active    int    `json:"active"`
		Keepalive int    `json:"keepalive"`
		MaxConns  int    `json:"max_conns"`
		Requests  int64  `json:"requests"`
		Responses struct {
			Responses1xx int64 `json:"1xx"`
			Responses2xx int64 `json:"2xx"`
			Responses3xx int64 `json:"3xx"`
			Responses4xx int64 `json:"4xx"`
			Responses5xx int64 `json:"5xx"`
			Total        int64 `json:"total"`
		} `json:"responses"`
		Sent         int64 `json:"sent"`
		Received     int64 `json:"received"`
		Fails        int64 `json:"fails"`
		Unavail      int64 `json:"unavail"`
		HealthChecks struct {
			Checks     int64 `json:"checks"`
			Fails      int64 `json:"fails"`
			Unhealthy  int64 `json:"unhealthy"`
			LastPassed *bool `json:"last_passed"`
		} `json:"health_checks"`
		Downtime     int64 `json:"downtime"`
		Downstart    int64 `json:"downstart"`
		Selected     int64 `json:"selected"`
		HeaderTime   int64 `json:"header_time"`
		ResponseTime int64 `json:"response_time"`
	} `json:"peers"`
	Keepalive int `json:"keepalive"`
	Zombies   int `json:"zombies"`
	Queue     struct {
		Size      int   `json:"size"`
		MaxSize   int   `json:"max_size"`
		Overflows int64 `json:"overflows"`
	} `json:"queue"`
}

type Exporter struct {
	URI string

	connectionsMetrics, sslMetrics, requestsMetrics, serverMetrics, upstreamMetrics map[string]*prometheus.Desc
}

func newCustomMetric(metricGroupName string, metricName string, docString string, labels []string) *prometheus.Desc {
	return prometheus.NewDesc(
		prometheus.BuildFQName(*metricsNamespace, metricGroupName, metricName),
		docString, labels, nil,
	)
}

func NewExporter(uri string) *Exporter {

	return &Exporter{
		URI: uri,
		connectionsMetrics: map[string]*prometheus.Desc{
			"accepted": newCustomMetric("connections", "accepted", "nginx connections", nil),
			"dropped":  newCustomMetric("connections", "dropped", "nginx connections", nil),
			"active":   newCustomMetric("connections", "active", "nginx connections", nil),
			"idle":     newCustomMetric("connections", "idle", "nginx connections", nil),
		},
		sslMetrics: map[string]*prometheus.Desc{
			"handshakes":        newCustomMetric("ssl", "handshakes", "nginx connections", nil),
			"handshakes_failed": newCustomMetric("ssl", "handshakes_failed", "nginx connections", nil),
			"session_reuses":    newCustomMetric("ssl", "session_reuses", "nginx connections", nil),
		},
		requestsMetrics: map[string]*prometheus.Desc{
			"total":   newCustomMetric("requests", "total", "nginx connections", nil),
			"current": newCustomMetric("requests", "current", "nginx connections", nil),
		},
		serverMetrics: map[string]*prometheus.Desc{
			"processing": newCustomMetric("server", "processing", "nginx connections", []string{"server"}),
			"requests":   newCustomMetric("server", "requests", "nginx connections", []string{"server"}),
			"discarded":  newCustomMetric("server", "discarded", "nginx connections", []string{"server"}),
			"received":   newCustomMetric("server", "received", "nginx connections", []string{"server"}),
			"sent":       newCustomMetric("server", "sent", "nginx connections", []string{"server"}),
			"responses":  newCustomMetric("server", "responses", "responses counter", []string{"server", "code"}),
		},
		upstreamMetrics: map[string]*prometheus.Desc{
			"requests":  newCustomMetric("upstream", "requests", "requests counter", []string{"server", "upstream"}),
			"fails":     newCustomMetric("upstream", "fails", "fails counter", []string{"server", "upstream"}),
			"received":  newCustomMetric("upstream", "received", "receive counter", []string{"server", "upstream"}),
			"sent":      newCustomMetric("upstream", "sent", "sent counter", []string{"server", "upstream"}),
			"downtime":  newCustomMetric("upstream", "downtime", "downtime counter", []string{"server", "upstream"}),
			"responses": newCustomMetric("upstream", "responses", "response counter", []string{"server", "upstream", "code"}),
		},
	}
}

func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	for _, m := range e.serverMetrics {
		ch <- m
	}
	for _, m := range e.connectionsMetrics {
		ch <- m
	}
	for _, m := range e.sslMetrics {
		ch <- m
	}
	for _, m := range e.requestsMetrics {
		ch <- m
	}
	for _, m := range e.upstreamMetrics {
		ch <- m
	}

}

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	// data, err := ioutil.ReadFile("./sample.json")
	body, err := fetchHTTP(e.URI, 2*time.Second)()
	if err != nil {
		log.Println("fetchHTTP failed", err)
		return
	}
	defer body.Close()

	data, err := ioutil.ReadAll(body)
	if err != nil {
		log.Println("ioutil.ReadAll failed", err)
		return
	}

	var nginxStats NginxStats
	err = json.Unmarshal(data, &nginxStats)
	if err != nil {
		log.Println("json.Unmarshal failed", err)
		return
	}

	// connections
	ch <- prometheus.MustNewConstMetric(e.connectionsMetrics["accepted"], prometheus.CounterValue, float64(nginxStats.Connections.Accepted))
	ch <- prometheus.MustNewConstMetric(e.connectionsMetrics["dropped"], prometheus.CounterValue, float64(nginxStats.Connections.Dropped))
	ch <- prometheus.MustNewConstMetric(e.connectionsMetrics["active"], prometheus.GaugeValue, float64(nginxStats.Connections.Active))
	ch <- prometheus.MustNewConstMetric(e.connectionsMetrics["idle"], prometheus.GaugeValue, float64(nginxStats.Connections.Idle))

	// ssl
	ch <- prometheus.MustNewConstMetric(e.sslMetrics["handshakes"], prometheus.CounterValue, float64(nginxStats.SSLs.Handshakes))
	ch <- prometheus.MustNewConstMetric(e.sslMetrics["handshakes_failed"], prometheus.CounterValue, float64(nginxStats.SSLs.HandshakesFailed))
	ch <- prometheus.MustNewConstMetric(e.sslMetrics["session_reuses"], prometheus.CounterValue, float64(nginxStats.SSLs.SessionReuses))

	// ServerZones
	for host, s := range nginxStats.ServerZones {
		ch <- prometheus.MustNewConstMetric(e.serverMetrics["processing"], prometheus.GaugeValue, float64(s.Processing), host)
		ch <- prometheus.MustNewConstMetric(e.serverMetrics["requests"], prometheus.CounterValue, float64(s.Requests), host)
		ch <- prometheus.MustNewConstMetric(e.serverMetrics["discarded"], prometheus.CounterValue, float64(s.Discarded), host)
		ch <- prometheus.MustNewConstMetric(e.serverMetrics["received"], prometheus.CounterValue, float64(s.Received), host)
		ch <- prometheus.MustNewConstMetric(e.serverMetrics["sent"], prometheus.CounterValue, float64(s.Sent), host)
		ch <- prometheus.MustNewConstMetric(e.serverMetrics["responses"], prometheus.CounterValue, float64(s.Responses.Responses1xx), host, "1xx")
		ch <- prometheus.MustNewConstMetric(e.serverMetrics["responses"], prometheus.CounterValue, float64(s.Responses.Responses2xx), host, "2xx")
		ch <- prometheus.MustNewConstMetric(e.serverMetrics["responses"], prometheus.CounterValue, float64(s.Responses.Responses3xx), host, "3xx")
		ch <- prometheus.MustNewConstMetric(e.serverMetrics["responses"], prometheus.CounterValue, float64(s.Responses.Responses4xx), host, "4xx")
		ch <- prometheus.MustNewConstMetric(e.serverMetrics["responses"], prometheus.CounterValue, float64(s.Responses.Responses5xx), host, "5xx")

	}

	// UpstreamZones
	for host, zone := range nginxStats.UpstreamZones {
		for _, p := range zone.Peers {
			upstream := p.Server
			ch <- prometheus.MustNewConstMetric(e.upstreamMetrics["requests"], prometheus.CounterValue, float64(p.Requests), host, upstream)
			ch <- prometheus.MustNewConstMetric(e.upstreamMetrics["sent"], prometheus.CounterValue, float64(p.Sent), host, upstream)
			ch <- prometheus.MustNewConstMetric(e.upstreamMetrics["received"], prometheus.CounterValue, float64(p.Received), host, upstream)
			ch <- prometheus.MustNewConstMetric(e.upstreamMetrics["downtime"], prometheus.CounterValue, float64(p.Downtime), host, upstream)
			ch <- prometheus.MustNewConstMetric(e.upstreamMetrics["fails"], prometheus.CounterValue, float64(p.Fails), host, upstream)
			ch <- prometheus.MustNewConstMetric(e.upstreamMetrics["responses"], prometheus.CounterValue, float64(p.Responses.Responses1xx), host, upstream, "1xx")
			ch <- prometheus.MustNewConstMetric(e.upstreamMetrics["responses"], prometheus.CounterValue, float64(p.Responses.Responses2xx), host, upstream, "2xx")
			ch <- prometheus.MustNewConstMetric(e.upstreamMetrics["responses"], prometheus.CounterValue, float64(p.Responses.Responses3xx), host, upstream, "3xx")
			ch <- prometheus.MustNewConstMetric(e.upstreamMetrics["responses"], prometheus.CounterValue, float64(p.Responses.Responses4xx), host, upstream, "4xx")
			ch <- prometheus.MustNewConstMetric(e.upstreamMetrics["responses"], prometheus.CounterValue, float64(p.Responses.Responses5xx), host, upstream, "5xx")
		}
	}

}

func fetchHTTP(uri string, timeout time.Duration) func() (io.ReadCloser, error) {
	http.DefaultClient.Timeout = timeout

	return func() (io.ReadCloser, error) {
		resp, err := http.DefaultClient.Get(uri)
		if err != nil {
			return nil, err
		}
		if !(resp.StatusCode >= 200 && resp.StatusCode < 300) {
			resp.Body.Close()
			return nil, fmt.Errorf("HTTP status %d", resp.StatusCode)
		}
		return resp.Body, nil
	}
}

var (
	showVersion      = flag.Bool("version", false, "Print version information.")
	listenAddress    = flag.String("telemetry.address", ":9913", "Address on which to expose metrics.")
	metricsEndpoint  = flag.String("telemetry.endpoint", "/metrics", "Path under which to expose metrics.")
	metricsNamespace = flag.String("metrics.namespace", "nginx", "Prometheus metrics namespace.")
	nginxScrapeURI   = flag.String("nginx.scrape_uri", "http://localhost/status", "URI to nginx stub status page")
	insecure         = flag.Bool("insecure", true, "Ignore server certificate if using https")
)

func init() {
	prometheus.MustRegister(version.NewCollector("nginx_plus_exporter"))
}

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Fprintln(os.Stdout, version.Print("Nginx plus exporter"))
		os.Exit(0)
	}

	log.Printf("Starting nginx plus exporter %s", version.Info())
	log.Printf("Build context %s", version.BuildContext())

	exporter := NewExporter(*nginxScrapeURI)
	prometheus.MustRegister(exporter)
	prometheus.Unregister(prometheus.NewProcessCollector(os.Getpid(), ""))
	prometheus.Unregister(prometheus.NewGoCollector())

	http.Handle(*metricsEndpoint, promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
			<head><title>Nginx Exporter</title></head>
			<body>
			<h1>Nginx Exporter</h1>
			<p><a href="` + *metricsEndpoint + `">Metrics</a></p>
			</body>
			</html>`))
	})

	log.Printf("Starting Server at : %s", *listenAddress)
	log.Printf("Metrics endpoint: %s", *metricsEndpoint)
	log.Printf("Metrics namespace: %s", *metricsNamespace)
	log.Printf("Scraping information from : %s", *nginxScrapeURI)
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
