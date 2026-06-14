package pvgis_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tamnd/pvgis-cli/pvgis"
)

func newTestClient(ts *httptest.Server) *pvgis.Client {
	cfg := pvgis.DefaultConfig()
	cfg.BaseURL = ts.URL
	cfg.Rate = 0
	cfg.Retries = 3
	return pvgis.NewClient(cfg)
}

const mockPVCalcResponse = `{
  "inputs": {
    "location": {"latitude": 45.0, "longitude": 8.0},
    "meteo_data": {"radiation_db": "PVGIS-SARAH2"},
    "mounting_system": {
      "fixed": {
        "slope": {"value": 35, "optimal": false},
        "azimuth": {"value": 0, "optimal": false}
      }
    },
    "pv_module": {"technology": "crystSi", "peak_power": 1.0, "system_loss": 14.0}
  },
  "outputs": {
    "totals": {
      "fixed": {
        "E_d": 3.38,
        "E_m": 102.87,
        "E_y": 1234.5,
        "H(i)_d": 4.21,
        "H(i)_m": 128.07,
        "H(i)_y": 1536.83,
        "SD_m": 7.55,
        "SD_y": 45.6,
        "l_aoi": -2.79,
        "l_spec": "-0.36%",
        "l_tgnoct": -5.24,
        "l_total": -21.12,
        "PR": 0.803
      }
    }
  },
  "meta": {
    "inputs": {},
    "outputs": {}
  }
}`

const mockMRCalcResponse = `{
  "inputs": {
    "location": {"latitude": 45.0, "longitude": 8.0}
  },
  "outputs": {
    "monthly": [
      {"month": 1, "H(h)": 37.52, "H(i)": 56.83, "Hb(i)": 38.28, "Hd(i)": 18.55},
      {"month": 2, "H(h)": 57.40, "H(i)": 79.44, "Hb(i)": 55.00, "Hd(i)": 24.44},
      {"month": 3, "H(h)": 95.42, "H(i)": 117.80, "Hb(i)": 83.86, "Hd(i)": 33.94},
      {"month": 4, "H(h)": 126.03, "H(i)": 139.13, "Hb(i)": 97.45, "Hd(i)": 41.68},
      {"month": 5, "H(h)": 163.31, "H(i)": 163.38, "Hb(i)": 112.15, "Hd(i)": 51.23},
      {"month": 6, "H(h)": 177.34, "H(i)": 170.53, "Hb(i)": 117.79, "Hd(i)": 52.74},
      {"month": 7, "H(h)": 190.09, "H(i)": 181.80, "Hb(i)": 131.79, "Hd(i)": 50.01},
      {"month": 8, "H(h)": 167.62, "H(i)": 170.65, "Hb(i)": 124.36, "Hd(i)": 46.29},
      {"month": 9, "H(h)": 117.51, "H(i)": 138.02, "Hb(i)": 102.77, "Hd(i)": 35.25},
      {"month": 10, "H(h)": 80.64, "H(i)": 109.67, "Hb(i)": 81.05, "Hd(i)": 28.62},
      {"month": 11, "H(h)": 43.80, "H(i)": 68.34, "Hb(i)": 48.58, "Hd(i)": 19.76},
      {"month": 12, "H(h)": 34.18, "H(i)": 52.85, "Hb(i)": 33.54, "Hd(i)": 19.31}
    ]
  },
  "meta": {}
}`

// TestPVCalcParsesYearlyEnergy checks that annual yield, daily energy, and PR
// are extracted from the PVcalc response.
func TestPVCalcParsesYearlyEnergy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, mockPVCalcResponse)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	r, err := c.PVCalc(context.Background(), 45.0, 8.0, 1.0, 14.0, 35, 0)
	if err != nil {
		t.Fatal(err)
	}
	if r.YearlyEnergy != 1234.5 {
		t.Errorf("YearlyEnergy = %f, want 1234.5", r.YearlyEnergy)
	}
	if r.DailyEnergy != 3.38 {
		t.Errorf("DailyEnergy = %f, want 3.38", r.DailyEnergy)
	}
	if r.PerformanceRatio != 0.803 {
		t.Errorf("PerformanceRatio = %f, want 0.803", r.PerformanceRatio)
	}
}

// TestPVCalcSetsUserAgent asserts every request carries a User-Agent header.
func TestPVCalcSetsUserAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua := r.Header.Get("User-Agent")
		if ua == "" {
			t.Error("request carried no User-Agent")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, mockPVCalcResponse)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	_, err := c.PVCalc(context.Background(), 45.0, 8.0, 1.0, 14.0, 35, 0)
	if err != nil {
		t.Fatal(err)
	}
}

// TestMonthlyParsesAllMonths verifies that all 12 monthly records are extracted.
func TestMonthlyParsesAllMonths(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, mockMRCalcResponse)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	months, err := c.Monthly(context.Background(), 45.0, 8.0, 35, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(months) != 12 {
		t.Fatalf("len(months) = %d, want 12", len(months))
	}
	if months[0].Month != 1 {
		t.Errorf("months[0].Month = %d, want 1", months[0].Month)
	}
	if months[0].Radiation != 56.83 {
		t.Errorf("months[0].Radiation = %f, want 56.83", months[0].Radiation)
	}
	if months[11].Month != 12 {
		t.Errorf("months[11].Month = %d, want 12", months[11].Month)
	}
}

// TestPVCalcRetriesOn503 verifies that the client retries on 5xx responses.
func TestPVCalcRetriesOn503(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, mockPVCalcResponse)
	}))
	defer srv.Close()

	cfg := pvgis.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Rate = 0
	cfg.Retries = 5
	c := pvgis.NewClient(cfg)

	start := time.Now()
	r, err := c.PVCalc(context.Background(), 45.0, 8.0, 1.0, 14.0, 35, 0)
	if err != nil {
		t.Fatal(err)
	}
	if r.YearlyEnergy != 1234.5 {
		t.Errorf("YearlyEnergy = %f after retries, want 1234.5", r.YearlyEnergy)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}

// TestPVCalcHTTPError verifies that non-200 HTTP status codes yield an error.
func TestPVCalcHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	_, err := c.PVCalc(context.Background(), 999.0, 999.0, 1.0, 14.0, 35, 0)
	if err == nil {
		t.Fatal("expected error on HTTP 400, got nil")
	}
}

// TestMonthlyDiffuseRadiation verifies diffuse radiation is extracted per month.
func TestMonthlyDiffuseRadiation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, mockMRCalcResponse)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	months, err := c.Monthly(context.Background(), 45.0, 8.0, 35, 0)
	if err != nil {
		t.Fatal(err)
	}
	// January diffuse should be 18.55
	if months[0].DiffuseRad != 18.55 {
		t.Errorf("months[0].DiffuseRad = %f, want 18.55", months[0].DiffuseRad)
	}
	// January direct should be 38.28
	if months[0].DirectRad != 38.28 {
		t.Errorf("months[0].DirectRad = %f, want 38.28", months[0].DirectRad)
	}
}
