// Package pvgis is the library behind the pvgis command line:
// the HTTP client, request shaping, and typed data models for the EU
// Photovoltaic Geographical Information System API (re.jrc.ec.europa.eu).
//
// PVGIS is a free EU-hosted service providing solar energy calculations for
// any geographic coordinate. No API key or registration is required. The Client
// paces requests, retries transient failures (429 and 5xx) with exponential
// backoff, and decodes the JSON responses into clean typed structs.
//
// Two operations are provided: annual PV energy yield (PVCalc) and monthly
// solar radiation data (Monthly).
package pvgis

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Host is the site this client talks to.
const Host = "re.jrc.ec.europa.eu"

// BaseURL is the API base path.
const BaseURL = "https://re.jrc.ec.europa.eu/api/v5_2"

// Config holds tunable knobs for the HTTP client.
type Config struct {
	BaseURL   string
	UserAgent string
	Rate      time.Duration
	Timeout   time.Duration
	Retries   int
}

// DefaultConfig returns sensible defaults for production use.
func DefaultConfig() Config {
	return Config{
		BaseURL:   BaseURL,
		UserAgent: "pvgis-cli/0.1.0 (github.com/tamnd/pvgis-cli)",
		Rate:      200 * time.Millisecond,
		Timeout:   60 * time.Second,
		Retries:   3,
	}
}

// Client talks to re.jrc.ec.europa.eu over HTTP.
type Client struct {
	cfg  Config
	http *http.Client
	mu   sync.Mutex
	last time.Time
}

// NewClient returns a Client configured with cfg.
func NewClient(cfg Config) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: cfg.Timeout},
	}
}

// PVResult holds the annual PV energy yield for a location.
type PVResult struct {
	Latitude         float64 `json:"latitude"`
	Longitude        float64 `json:"longitude"`
	PeakPower        float64 `json:"peak_power_kw,omitempty"`
	Loss             float64 `json:"loss_pct,omitempty"`
	YearlyEnergy     float64 `json:"yearly_energy_kwh"`
	DailyEnergy      float64 `json:"daily_energy_kwh"`
	PerformanceRatio float64 `json:"performance_ratio"`
}

// MonthlyData holds solar radiation data for one calendar month.
type MonthlyData struct {
	Month      int     `json:"month"`
	Radiation  float64 `json:"radiation_kwh_m2_day"` // H(i) irradiation on fixed plane
	DirectRad  float64 `json:"direct_radiation,omitempty"`
	DiffuseRad float64 `json:"diffuse_radiation,omitempty"`
}

// --- wire types ---

type wirePVResp struct {
	Inputs struct {
		Location struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		} `json:"location"`
	} `json:"inputs"`
	Outputs struct {
		Totals struct {
			Fixed struct {
				Ey float64 `json:"E_y"`
				Ed float64 `json:"E_d"`
				PR float64 `json:"PR"`
			} `json:"fixed"`
		} `json:"totals"`
	} `json:"outputs"`
}

type wireMRResp struct {
	Outputs struct {
		Monthly []struct {
			Month float64 `json:"month"`
			Hi    float64 `json:"H(i)"`
			HbI   float64 `json:"Hb(i)"`
			HdI   float64 `json:"Hd(i)"`
		} `json:"monthly"`
	} `json:"outputs"`
}

// PVCalc returns the annual PV energy yield for the given coordinates.
// peakPower is installed peak power in kWp; loss is system loss percentage.
// angle is tilt in degrees; aspect is azimuth (0=south, -90=east, 90=west).
func (c *Client) PVCalc(ctx context.Context, lat, lon, peakPower, loss float64, angle, aspect int) (*PVResult, error) {
	u := fmt.Sprintf(
		"%s/PVcalc?lat=%s&lon=%s&peakpower=%s&loss=%s&angle=%d&aspect=%d&outputformat=json",
		c.cfg.BaseURL,
		strconv.FormatFloat(lat, 'f', -1, 64),
		strconv.FormatFloat(lon, 'f', -1, 64),
		strconv.FormatFloat(peakPower, 'f', -1, 64),
		strconv.FormatFloat(loss, 'f', -1, 64),
		angle,
		aspect,
	)
	b, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}
	var resp wirePVResp
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("decode PVcalc response: %w", err)
	}
	return &PVResult{
		Latitude:         resp.Inputs.Location.Latitude,
		Longitude:        resp.Inputs.Location.Longitude,
		PeakPower:        peakPower,
		Loss:             loss,
		YearlyEnergy:     resp.Outputs.Totals.Fixed.Ey,
		DailyEnergy:      resp.Outputs.Totals.Fixed.Ed,
		PerformanceRatio: resp.Outputs.Totals.Fixed.PR,
	}, nil
}

// Monthly returns monthly solar radiation data for the given coordinates.
// angle is tilt in degrees; aspect is azimuth (0=south).
func (c *Client) Monthly(ctx context.Context, lat, lon float64, angle, aspect int) ([]MonthlyData, error) {
	u := fmt.Sprintf(
		"%s/MRcalc?lat=%s&lon=%s&angle=%d&aspect=%d&outputformat=json&mstartyear=2005&mendyear=2020",
		c.cfg.BaseURL,
		strconv.FormatFloat(lat, 'f', -1, 64),
		strconv.FormatFloat(lon, 'f', -1, 64),
		angle,
		aspect,
	)
	b, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}
	var resp wireMRResp
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("decode MRcalc response: %w", err)
	}
	out := make([]MonthlyData, 0, len(resp.Outputs.Monthly))
	for _, m := range resp.Outputs.Monthly {
		out = append(out, MonthlyData{
			Month:      int(m.Month),
			Radiation:  m.Hi,
			DirectRad:  m.HbI,
			DiffuseRad: m.HdI,
		})
	}
	return out, nil
}

// get fetches url and returns the response body. It paces and retries.
func (c *Client) get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.cfg.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) ([]byte, bool, error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace blocks until at least Rate has passed since the previous request.
func (c *Client) pace() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cfg.Rate <= 0 {
		return
	}
	if wait := c.cfg.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}
