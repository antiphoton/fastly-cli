package testutil

import (
	"errors"

	"github.com/fastly/go-fastly/v6/fastly"
)

// Err represents a generic error.
var Err = errors.New("test error")

// ListVersions returns a list of service versions in different states.
//
// The first element is active, the second is locked, the third is editable.
//
// NOTE: consult the entire test suite before adding any new entries to the
// returned type as the tests currently use testutil.CloneVersionResult() as a
// way of making the test output and expectations as accurate as possible.
func ListVersions(i *fastly.ListVersionsInput) ([]*fastly.Version, error) {
	return []*fastly.Version{
		{
			ServiceID: i.ServiceID,
			Number:    1,
			Active:    true,
			UpdatedAt: MustParseTimeRFC3339("2000-01-01T01:00:00Z"),
		},
		{
			ServiceID: i.ServiceID,
			Number:    2,
			Active:    false,
			Locked:    true,
			UpdatedAt: MustParseTimeRFC3339("2000-01-02T01:00:00Z"),
		},
		{
			ServiceID: i.ServiceID,
			Number:    3,
			Active:    false,
			UpdatedAt: MustParseTimeRFC3339("2000-01-03T01:00:00Z"),
		},
	}, nil
}

// ListVersionsError returns a generic error message when attempting to list
// service versions.
func ListVersionsError(_ *fastly.ListVersionsInput) ([]*fastly.Version, error) {
	return nil, Err
}

// CloneVersionResult returns a function which returns a specific cloned version.
func CloneVersionResult(version int) func(i *fastly.CloneVersionInput) (*fastly.Version, error) {
	return func(i *fastly.CloneVersionInput) (*fastly.Version, error) {
		return &fastly.Version{
			ServiceID: i.ServiceID,
			Number:    version,
		}, nil
	}
}

// CloneVersionError returns a generic error message when attempting to clone a
// service version.
func CloneVersionError(_ *fastly.CloneVersionInput) (*fastly.Version, error) {
	return nil, Err
}
