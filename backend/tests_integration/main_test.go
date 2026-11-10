//go:build integration

package tests_integration

import (
	"os"
	"testing"

	"github.com/kaanunsel/social-event-mapper/backend/tests_integration/common"
)

func TestMain(m *testing.M) {
	os.Exit(common.Run(m))
}
