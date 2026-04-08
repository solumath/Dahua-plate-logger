package logger

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/icholy/digest"
)

const (
	scannerBufSize        = 1 << 20
	maxJSONLines          = 500
	dialTimeout           = 10 * time.Second
	responseHeaderTimeout = 30 * time.Second
)

// CameraConfig holds connection details for a single camera.
type CameraConfig struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
	User string `yaml:"user"`
	Pass string `yaml:"pass"`
}

// Event is a parsed plate detection event.
type Event struct {
	UTC        int64
	UTCMS      int64
	Plate      string
	Camera     string
	ReceivedAt time.Time
}

// Time returns the server time when the event was received.
func (e Event) Time() time.Time {
	return e.ReceivedAt
}

type rawEvent struct {
	UTC      int64 `json:"UTC"`
	UTCMS    int64 `json:"UTCMS"`
	ObjectID int64 `json:"ObjectID"`
	Object   struct {
		ObjectType string `json:"ObjectType"`
		Text       string `json:"Text"`
	} `json:"Object"`
	TrafficCar struct {
		PlateNumber string `json:"PlateNumber"`
	} `json:"TrafficCar"`
}

// CameraStream holds a persistent HTTP client for one camera.
// Create once per camera; call Connect in a reconnect loop.
type CameraStream struct {
	cameraConfig CameraConfig
	client       *http.Client
	raw          io.Writer
	onEvent      func(camera, plate string)
	onConnect    func()
}

// NewCameraStream validates the config and creates a reusable HTTP client.
func NewCameraStream(cameraConfig CameraConfig) (*CameraStream, error) {
	if cameraConfig.Name == "" {
		return nil, fmt.Errorf("camera name is required")
	}
	if cameraConfig.URL == "" {
		return nil, fmt.Errorf("camera %q: url is required", cameraConfig.Name)
	}
	if cameraConfig.User == "" {
		return nil, fmt.Errorf("camera %q: user is required", cameraConfig.Name)
	}
	base := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   dialTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ResponseHeaderTimeout: responseHeaderTimeout,
	}
	return &CameraStream{
		cameraConfig: cameraConfig,
		client: &http.Client{
			Transport: &digest.Transport{
				Username:  cameraConfig.User,
				Password:  cameraConfig.Pass,
				Transport: base,
			},
		},
	}, nil
}

func (cs *CameraStream) Name() string         { return cs.cameraConfig.Name }
func (cs *CameraStream) Config() CameraConfig { return cs.cameraConfig }

// SetRawWriter enables raw stream logging to w (shared across all cameras).
func (cs *CameraStream) SetRawWriter(w io.Writer) { cs.raw = w }

// SetOnEvent registers a callback invoked after each successful plate insert.
func (cs *CameraStream) SetOnEvent(fn func(string, string)) { cs.onEvent = fn }

// SetOnConnect registers a callback invoked once the HTTP 200 is received.
func (cs *CameraStream) SetOnConnect(fn func()) { cs.onConnect = fn }

// Connect opens one streaming connection and processes events until it drops.
func (cs *CameraStream) Connect(ctx context.Context, store *Store) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cs.cameraConfig.URL, nil)
	if err != nil {
		return err
	}

	slog.Info("connecting", "camera", cs.cameraConfig.Name, "url", cs.cameraConfig.URL)
	resp, err := cs.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	slog.Info("connected", "camera", cs.cameraConfig.Name, "status", resp.StatusCode)
	if cs.onConnect != nil {
		cs.onConnect()
	}

	var body io.Reader = resp.Body
	if cs.raw != nil {
		body = io.TeeReader(resp.Body, cs.raw)
	}

	return iterLines(body, cs.cameraConfig.Name, func(e Event) {
		if err := store.Insert(e); err != nil {
			slog.Error("insert failed", "camera", cs.cameraConfig.Name, "plate", e.Plate, "err", err)
			return
		}
		if cs.onEvent != nil {
			cs.onEvent(e.Camera, e.Plate)
		}
		if isValidPlate(e.Plate) {
			slog.Info("plate logged",
				"camera", cs.cameraConfig.Name,
				"plate", e.Plate,
				"time", e.Time().Format("2006-01-02 15:04:05.000"),
			)
		} else {
			slog.Debug("malformed plate",
				"camera", cs.cameraConfig.Name,
				"plate", e.Plate,
				"time", e.Time().Format("2006-01-02 15:04:05.000"),
			)
		}
	})
}

func iterLines(r io.Reader, camera string, onEvent func(Event)) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, scannerBufSize), scannerBufSize)

	var collecting bool
	var braceDepth int
	var jsonLines []string

	for scanner.Scan() {
		line := scanner.Text()

		if collecting {
			if len(jsonLines) >= maxJSONLines {
				slog.Warn("JSON object exceeded line cap, discarding", "camera", camera, "lines", len(jsonLines))
				collecting = false
				jsonLines = jsonLines[:0]
				braceDepth = 0
				continue
			}
			jsonLines = append(jsonLines, line)
			braceDepth += braceDepthDelta(line)
			if braceDepth <= 0 {
				if e, ok := parseEvent(strings.Join(jsonLines, "\n"), camera); ok {
					e.ReceivedAt = time.Now()
					onEvent(e)
				}
				collecting = false
				jsonLines = jsonLines[:0]
				braceDepth = 0
			}
			continue
		}

		if strings.HasPrefix(line, "Code=TrafficJunction;action=Pulse") {
			idx := strings.Index(line, "data=")
			if idx == -1 {
				slog.Warn("no data= in Code line", "camera", camera, "line", line[:min(120, len(line))])
				continue
			}
			first := line[idx+len("data="):]
			jsonLines = []string{first}
			braceDepth = braceDepthDelta(first)
			collecting = braceDepth > 0
		} else if strings.HasPrefix(line, "Code=") {
			slog.Log(context.Background(), LevelAll, "camera event",
				"camera", camera,
				"line", line[:min(200, len(line))],
			)
		}
	}

	return scanner.Err()
}

// braceDepthDelta counts net brace depth in a line, ignoring braces inside strings.
func braceDepthDelta(line string) int {
	depth := 0
	inStr := false
	escaped := false
	for _, c := range line {
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && inStr {
			escaped = true
			continue
		}
		switch c {
		case '"':
			inStr = !inStr
		case '{':
			if !inStr {
				depth++
			}
		case '}':
			if !inStr {
				depth--
			}
		}
	}
	return depth
}

func parseEvent(jsonStr, camera string) (Event, bool) {
	var raw rawEvent
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		slog.Warn("JSON parse error", "camera", camera, "err", err, "raw", jsonStr[:min(200, len(jsonStr))])
		return Event{}, false
	}

	plate := ""
	if raw.Object.ObjectType == "Plate" {
		plate = raw.Object.Text
	}
	if plate == "" {
		plate = raw.TrafficCar.PlateNumber
	}
	if plate == "" {
		slog.Debug("event with no plate, skipping", "camera", camera, "objectID", raw.ObjectID)
		return Event{}, false
	}

	return Event{
		UTC:    raw.UTC,
		UTCMS:  raw.UTCMS,
		Plate:  plate,
		Camera: camera,
	}, true
}
