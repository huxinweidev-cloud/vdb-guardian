package inspection

import "testing"

func TestMapMilvusFieldToPGVectorSupportsCoreTypes(t *testing.T) {
	tests := []struct {
		name        string
		field       MilvusFieldPlan
		wantType    string
		wantLevel   string
		wantWarning bool
	}{
		{
			name:      "bool",
			field:     MilvusFieldPlan{Name: "is_active", SourceType: MilvusDataTypeBool},
			wantType:  "boolean",
			wantLevel: SupportLevelSupported,
		},
		{
			name:      "int64",
			field:     MilvusFieldPlan{Name: "tenant_id", SourceType: MilvusDataTypeInt64},
			wantType:  "bigint",
			wantLevel: SupportLevelSupported,
		},
		{
			name:      "varchar",
			field:     MilvusFieldPlan{Name: "title", SourceType: MilvusDataTypeVarChar, MaxLength: 128},
			wantType:  "varchar(128)",
			wantLevel: SupportLevelSupported,
		},
		{
			name:      "json",
			field:     MilvusFieldPlan{Name: "metadata", SourceType: MilvusDataTypeJSON},
			wantType:  "jsonb",
			wantLevel: SupportLevelSupported,
		},
		{
			name:      "float vector",
			field:     MilvusFieldPlan{Name: "embedding", SourceType: MilvusDataTypeFloatVector, Dimension: 8},
			wantType:  "vector(8)",
			wantLevel: SupportLevelSupported,
		},
		{
			name:        "sparse vector",
			field:       MilvusFieldPlan{Name: "sparse", SourceType: MilvusDataTypeSparseFloatVector},
			wantType:    "jsonb",
			wantLevel:   SupportLevelDegraded,
			wantWarning: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := MapMilvusFieldToPGVector(tt.field)
			if target.TargetType != tt.wantType {
				t.Fatalf("target type = %q, want %q", target.TargetType, tt.wantType)
			}
			if target.SupportLevel != tt.wantLevel {
				t.Fatalf("support level = %q, want %q", target.SupportLevel, tt.wantLevel)
			}
			if (target.Warning != "") != tt.wantWarning {
				t.Fatalf("warning = %q, want warning presence %v", target.Warning, tt.wantWarning)
			}
		})
	}
}

func TestBuildMilvusInspectionSummaryCountsWarningsAndUnsupportedFeatures(t *testing.T) {
	plan := MilvusInspectionPlan{
		Collections: []MilvusCollectionPlan{
			{
				Name: "supported_items",
				Fields: []MilvusFieldPlan{
					{Name: "id", SupportLevel: SupportLevelSupported},
					{Name: "embedding", SupportLevel: SupportLevelSupported},
				},
			},
			{
				Name:     "degraded_items",
				Warnings: []string{"partition metadata is preserved only in the plan"},
				Fields: []MilvusFieldPlan{
					{Name: "sparse", SupportLevel: SupportLevelDegraded, Warning: "sparse vector maps to jsonb until sparsevec support is enabled"},
				},
				Indexes: []MilvusIndexPlan{
					{Field: "embedding", SupportLevel: SupportLevelUnsupported, Warning: "index type is metadata-only"},
				},
			},
		},
	}

	summary := BuildMilvusInspectionSummary(plan.Collections)
	if summary.CollectionCount != 2 {
		t.Fatalf("collection count = %d, want 2", summary.CollectionCount)
	}
	if summary.SupportedCollectionCount != 1 {
		t.Fatalf("supported collection count = %d, want 1", summary.SupportedCollectionCount)
	}
	if summary.WarningCount != 3 {
		t.Fatalf("warning count = %d, want 3", summary.WarningCount)
	}
	if summary.UnsupportedFeatureCount != 1 {
		t.Fatalf("unsupported count = %d, want 1", summary.UnsupportedFeatureCount)
	}
}
