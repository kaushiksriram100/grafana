package sqlstore

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/go-xorm/xorm"
	"github.com/grafana/grafana/pkg/bus"
	"github.com/grafana/grafana/pkg/components/formatter"
	"github.com/grafana/grafana/pkg/components/simplejson"
	m "github.com/grafana/grafana/pkg/models"

	diff "github.com/yudai/gojsondiff"
	deltaFormatter "github.com/yudai/gojsondiff/formatter"
)

// TODO(ben) should this go in models?
var ErrUnsupportedDiffType = errors.New("sqlstore: unsupported diff type")

func init() {
	bus.AddHandler("sql", CompareDashboardVersionsCommand)
	bus.AddHandler("sql", GetDashboardVersion)
	bus.AddHandler("sql", GetDashboardVersions)
	bus.AddHandler("sql", RestoreDashboardVersion)
}

// CompareDashboardVersionsCommand computes the JSON diff of two versions,
// assigning the delta of the diff to the `Delta` field.
func CompareDashboardVersionsCommand(cmd *m.CompareDashboardVersionsCommand) error {
	// Find original version
	original, err := getDashboardVersion(cmd.DashboardId, cmd.Original)
	if err != nil {
		return err
	}

	newDashboard, err := getDashboardVersion(cmd.DashboardId, cmd.New)
	if err != nil {
		return err
	}

	left, jsonDiff, err := getDiff(original, newDashboard)
	if err != nil {
		return err
	}
	switch cmd.DiffType {
	case m.DiffDelta:
		deltaOutput, err := deltaFormatter.NewDeltaFormatter().Format(jsonDiff)
		if err != nil {
			return err
		}
		cmd.Delta = []byte(deltaOutput)

	case m.DiffJSON:
		jsonOutput, err := formatter.NewJSONFormatter(left).Format(jsonDiff)
		if err != nil {
			return err
		}
		cmd.Delta = []byte(jsonOutput)

	case m.DiffBasic:
		basicOutput, err := formatter.NewBasicFormatter(left).Format(jsonDiff)
		if err != nil {
			return err
		}
		cmd.Delta = basicOutput

	default:
		return ErrUnsupportedDiffType
	}

	return nil
}

// GetDashboardVersion gets the dashboard version for the given dashboard ID
// and version number.
func GetDashboardVersion(query *m.GetDashboardVersionCommand) error {
	result, err := getDashboardVersion(query.DashboardId, query.Version)
	if err != nil {
		return err
	}

	query.Result = result
	return nil
}

// GetDashboardVersions gets all dashboard versions for the given dashboard ID.
func GetDashboardVersions(query *m.GetDashboardVersionsCommand) error {
	order := ""
	if query.OrderBy != "" {
		order = " desc"
	}
	err := x.In("dashboard_id", query.DashboardId).
		OrderBy(query.OrderBy+order).
		Limit(query.Limit, query.Start).
		Find(&query.Result)
	if err != nil {
		return err
	}

	if len(query.Result) < 1 {
		return m.ErrNoVersionsForDashboardId
	}
	return nil
}

// RestoreDashboardVersion restores the dashboard data to the given version.
func RestoreDashboardVersion(cmd *m.RestoreDashboardVersionCommand) error {
	return inTransaction(func(sess *xorm.Session) error {
		// Check if dashboard version exists in dashboard_version table
		dashboardVersion, err := getDashboardVersion(cmd.DashboardId, cmd.Version)
		if err != nil {
			return err
		}

		dashboard, err := getDashboard(cmd.DashboardId)
		if err != nil {
			return err
		}

		version, err := getMaxVersion(sess, dashboard.Id)
		if err != nil {
			return err
		}

		// revert and save to a new dashboard version
		dashboard.Data = dashboardVersion.Data
		dashboard.Updated = time.Now()
		dashboard.UpdatedBy = cmd.UserId
		dashboard.Version = version
		dashboard.Data.Set("version", dashboard.Version)
		// TODO(ben): decide when this should be cleared, or if it should exist at all
		dashboard.Data.Set("restoredFrom", cmd.Version)
		affectedRows, err := sess.Id(dashboard.Id).Update(dashboard)
		if err != nil {
			return err
		}
		if affectedRows == 0 {
			return m.ErrDashboardNotFound
		}

		// save that version a new version
		dashVersion := &m.DashboardVersion{
			DashboardId:   dashboard.Id,
			ParentVersion: cmd.Version,
			RestoredFrom:  cmd.Version,
			Version:       dashboard.Version,
			Created:       time.Now(),
			CreatedBy:     dashboard.UpdatedBy,
			Message:       "",
			Data:          dashboard.Data,
		}
		affectedRows, err = sess.Insert(dashVersion)
		if err != nil {
			return err
		}
		if affectedRows == 0 {
			return m.ErrDashboardNotFound
		}

		cmd.Result = dashboard
		return nil
	})
}

// getDashboardVersion is a helper function that gets the dashboard version for
// the given dashboard ID and version ID.
func getDashboardVersion(dashboardId int64, version int) (*m.DashboardVersion, error) {
	dashboardVersion := m.DashboardVersion{}
	has, err := x.Where("dashboard_id=? AND version=?", dashboardId, version).Get(&dashboardVersion)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, m.ErrDashboardVersionNotFound
	}

	dashboardVersion.Data.Set("id", dashboardVersion.DashboardId)
	return &dashboardVersion, nil
}

// getDashboard gets a dashboard by ID. Used for retrieving the dashboard
// associated with dashboard versions.
func getDashboard(dashboardId int64) (*m.Dashboard, error) {
	dashboard := m.Dashboard{Id: dashboardId}
	has, err := x.Get(&dashboard)
	if err != nil {
		return nil, err
	}
	if has == false {
		return nil, m.ErrDashboardNotFound
	}
	return &dashboard, nil
}

// getDiff computes the diff of two dashboard versions.
func getDiff(originalDash, newDash *m.DashboardVersion) (interface{}, diff.Diff, error) {
	leftBytes, err := simplejson.NewFromAny(originalDash).Encode()
	if err != nil {
		return nil, nil, err
	}

	rightBytes, err := simplejson.NewFromAny(newDash).Encode()
	if err != nil {
		return nil, nil, err
	}

	jsonDiff, err := diff.New().Compare(leftBytes, rightBytes)
	if err != nil {
		return nil, nil, err
	}

	if !jsonDiff.Modified() {
		return nil, nil, nil
	}

	left := make(map[string]interface{})
	err = json.Unmarshal(leftBytes, &left)
	return left, jsonDiff, nil
}

type version struct {
	Max int
}

// getMaxVersion returns the highest version number in the `dashboard_version`
// table
func getMaxVersion(sess *xorm.Session, dashboardId int64) (int, error) {
	v := version{}
	has, err := sess.Table("dashboard_version").
		Select("MAX(version) AS max"). // thank you sqlite3 :()
		Where("dashboard_id = ?", dashboardId).
		Get(&v)
	if !has {
		return 0, m.ErrDashboardNotFound
	}
	if err != nil {
		return 0, err
	}

	v.Max++
	return v.Max, nil
}
