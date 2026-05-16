package migration

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strconv"
)

// FullRecordArtifactBuildOptions configures construction of a local full-record
// artifact from normalized migration records.
type FullRecordArtifactBuildOptions struct {
	System            string
	Collection        string
	RecordMappingPath string
}

// BuildFullRecordArtifact converts normalized migration records into a stable
// full-record artifact for equality comparison.
func BuildFullRecordArtifact(records []VectorMigrationRecord, options FullRecordArtifactBuildOptions) (FullRecordArtifact, error) {
	if options.System == "" {
		return FullRecordArtifact{}, fmt.Errorf("system is required")
	}
	if options.Collection == "" {
		return FullRecordArtifact{}, fmt.Errorf("collection is required")
	}
	artifactRecords := make([]FullRecordArtifactRecord, len(records))
	seen := make(map[string]struct{}, len(records))
	for index, record := range records {
		artifactRecord, err := fullRecordArtifactRecordFromMigrationRecord(record)
		if err != nil {
			return FullRecordArtifact{}, err
		}
		if _, exists := seen[artifactRecord.ID]; exists {
			return FullRecordArtifact{}, fmt.Errorf("duplicate record id %q", artifactRecord.ID)
		}
		seen[artifactRecord.ID] = struct{}{}
		artifactRecords[index] = artifactRecord
	}
	sort.SliceStable(artifactRecords, func(i, j int) bool {
		return artifactRecords[i].ID < artifactRecords[j].ID
	})
	return FullRecordArtifact{
		SchemaVersion:     FullRecordArtifactVersion,
		System:            options.System,
		Collection:        options.Collection,
		RecordMappingPath: options.RecordMappingPath,
		Records:           artifactRecords,
	}, nil
}

func fullRecordArtifactRecordFromMigrationRecord(record VectorMigrationRecord) (FullRecordArtifactRecord, error) {
	if record.ID == "" {
		return FullRecordArtifactRecord{}, fmt.Errorf("record id is required")
	}
	if len(record.Vector) == 0 {
		return FullRecordArtifactRecord{}, fmt.Errorf("vector is required for record %q", record.ID)
	}
	vectorHash, err := hashFullRecordVector(record.Vector)
	if err != nil {
		return FullRecordArtifactRecord{}, fmt.Errorf("hash vector for record %q: %w", record.ID, err)
	}
	return FullRecordArtifactRecord{
		ID:              record.ID,
		VectorHash:      vectorHash,
		VectorDimension: len(record.Vector),
		Scalars:         copyMigrationValueMap(record.Scalars),
		DynamicMetadata: copyMigrationValueMap(record.DynamicMetadata),
		Partition:       record.Partition,
	}, nil
}

func hashFullRecordVector(vector []float64) (string, error) {
	hasher := sha256.New()
	for index, value := range vector {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return "", fmt.Errorf("non-finite vector value at dimension %d", index)
		}
		if index > 0 {
			hasher.Write([]byte(","))
		}
		canonical := float32(value)
		hasher.Write([]byte(strconv.FormatFloat(float64(canonical), 'g', -1, 32)))
	}
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil)), nil
}
