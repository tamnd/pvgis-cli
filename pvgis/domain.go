// domain.go exposes pvgis as a kit Domain: a driver that a multi-domain
// host (ant) enables with a single blank import,
//
//	import _ "github.com/tamnd/pvgis-cli/pvgis"
//
// exactly as a database/sql program enables a driver with `import _
// "github.com/lib/pq"`. The init below registers it; the host then dereferences
// pvgis:// URIs by routing to the operations Register installs. The same
// Domain also builds the standalone pvgis binary (see cli.NewApp), so the
// binary and a host share one source of truth.
package pvgis

import (
	"context"
	"fmt"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

func init() { kit.Register(Domain{}) }

// Domain is the pvgis driver. It carries no state; the per-run client is
// built by the factory Register hands kit.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against, and
// the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "pvgis",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "pvgis",
			Short:  "Fetch solar energy data from the EU PVGIS API.",
			Long: `pvgis fetches solar energy calculations from the EU Photovoltaic Geographical
Information System (re.jrc.ec.europa.eu). No API key required.

It computes annual PV energy yield and monthly solar radiation for any
geographic coordinate on Earth.`,
			Site: Host,
			Repo: "https://github.com/tamnd/pvgis-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	kit.Handle(app, kit.OpMeta{
		Name: "pv", Group: "read", Single: true,
		Summary: "Calculate annual PV energy yield for a location",
		URIType: "pv",
	}, getPV)

	kit.Handle(app, kit.OpMeta{
		Name: "monthly", Group: "read", List: true,
		Summary: "Fetch monthly solar radiation data for a location",
		URIType: "monthly",
	}, getMonthly)
}

// newClient builds the client from the host-resolved config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := DefaultConfig()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.Timeout = cfg.Timeout
	}
	return NewClient(c), nil
}

// --- inputs ---

type pvInput struct {
	Lat    float64 `kit:"flag" help:"latitude"`
	Lon    float64 `kit:"flag" help:"longitude"`
	Peak   float64 `kit:"flag" help:"installed peak power in kWp"`
	Loss   float64 `kit:"flag" help:"system loss percentage"`
	Angle  int     `kit:"flag" help:"tilt angle in degrees"`
	Aspect int     `kit:"flag" help:"azimuth: 0=south, -90=east, 90=west"`
	Client *Client `kit:"inject"`
}

type monthlyInput struct {
	Lat    float64 `kit:"flag" help:"latitude"`
	Lon    float64 `kit:"flag" help:"longitude"`
	Angle  int     `kit:"flag" help:"tilt angle in degrees"`
	Aspect int     `kit:"flag" help:"azimuth: 0=south, -90=east, 90=west"`
	Client *Client `kit:"inject"`
}

// --- handlers ---

func getPV(ctx context.Context, in pvInput, emit func(*PVResult) error) error {
	peak := in.Peak
	if peak <= 0 {
		peak = 1.0
	}
	loss := in.Loss
	if loss <= 0 {
		loss = 14.0
	}
	angle := in.Angle
	if angle == 0 {
		angle = 35
	}
	r, err := in.Client.PVCalc(ctx, in.Lat, in.Lon, peak, loss, angle, in.Aspect)
	if err != nil {
		return mapErr(err)
	}
	return emit(r)
}

func getMonthly(ctx context.Context, in monthlyInput, emit func(*MonthlyData) error) error {
	angle := in.Angle
	if angle == 0 {
		angle = 35
	}
	months, err := in.Client.Monthly(ctx, in.Lat, in.Lon, angle, in.Aspect)
	if err != nil {
		return mapErr(err)
	}
	for i := range months {
		if err := emit(&months[i]); err != nil {
			return err
		}
	}
	return nil
}

// mapErr converts a library error into the kit error kind with the right exit code.
func mapErr(err error) error {
	msg := err.Error()
	if len(msg) > 7 && msg[:7] == "http 40" {
		return errs.NotFound("%s", msg)
	}
	return fmt.Errorf("%w", err)
}
