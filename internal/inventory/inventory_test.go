package inventory_test

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/inventory"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func TestXLSX500RowImportAndClaimCollisions(t *testing.T) {
	// 1. Build a 500-row .xlsx workbook with 1 intentional duplicate on row 501
	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	headers := []string{"name", "address", "port", "user", "credential", "zone", "rack", "labels"}
	for col, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(col+1, 1)
		_ = f.SetCellValue(sheet, cell, h)
	}
	for i := 1; i <= 500; i++ {
		r := i + 1
		name := fmt.Sprintf("node-%03d", i)
		addr := fmt.Sprintf("10.10.%d.%d", i/250, i%250+1)
		if i == 500 {
			name = "node-001" // intentional duplicate at row 501!
		}
		vals := []any{name, addr, 22, "ops", "ssh-prod", "az1", "rack1", "role=worker;env=prod"}
		for col, v := range vals {
			cell, _ := excelize.CoordinatesToCellName(col+1, r)
			_ = f.SetCellValue(sheet, cell, v)
		}
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	res, err := inventory.ParseXLSX(&buf)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("ParseXLSX: %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("500-row xlsx import took %s (> 2s DoD)", elapsed)
	}
	if len(res.Rows) != 499 || len(res.Errors) != 1 || res.Errors[0].RowNumber != 501 {
		t.Fatalf("expected 499 valid + 1 error at row 501, got rows=%d errs=%+v", len(res.Rows), res.Errors)
	}

	// 2. R2 Sonar/Attu port 9000 collision + Docker Bridge CIDR collision
	host := domain.Host{
		ID: "host_01", Name: "app-01",
		Facts: &sdk.FactsView{
			MemMB: 16384,
			NICs:  []sdk.NICView{{Name: "eth0", Addresses: []string{"10.20.0.10/24"}}},
		},
	}
	existing := []domain.HostClaim{{
		InstanceID: "inst_sonar", HostID: "host_01", Role: "app",
		Ports:   []sdk.PortClaim{{Port: 9000, Proto: "tcp"}},
		Paths:   []string{"/data/sonar"},
		Subnets: []string{"10.240.1.0/24"},
		MemMB:   4096,
	}}
	// Port 9000 conflict
	if err := inventory.CheckClaimConflicts(host, existing, sdk.HostRoleClaims{Ports: []sdk.PortClaim{{Port: 9000}}}, ""); !sdk.IsCode(err, sdk.CodeHostClaimConflict) {
		t.Fatalf("expected KI-HOST-0002 port conflict, got %v", err)
	}
	// Docker Bridge subnet collision with existing instance
	if err := inventory.CheckClaimConflicts(host, existing, sdk.HostRoleClaims{Subnets: []string{"10.240.1.128/25"}}, ""); !sdk.IsCode(err, sdk.CodeHostClaimConflict) {
		t.Fatalf("expected KI-HOST-0002 subnet conflict, got %v", err)
	}
	// Docker Bridge subnet collision with host physical NIC
	if err := inventory.CheckClaimConflicts(host, existing, sdk.HostRoleClaims{Subnets: []string{"10.20.0.0/16"}}, ""); !sdk.IsCode(err, sdk.CodeHostClaimConflict) {
		t.Fatalf("expected KI-HOST-0002 NIC subnet conflict, got %v", err)
	}
}
