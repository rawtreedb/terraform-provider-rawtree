package s3_ingestion

import (
	"testing"

	"github.com/rawtreedb/terraform-provider-rawtree/internal/util"
)

func TestSanitizeResourceName_Compat(t *testing.T) {
	t.Parallel()

	result := util.SanitizeResourceName("myorg-myproject-events")
	if len(result) > 40 {
		t.Errorf("result too long: %d chars (%s)", len(result), result)
	}
	if len(result) < 10 {
		t.Errorf("result too short: %d chars (%s)", len(result), result)
	}
}
