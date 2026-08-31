package modulith

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
)

// sarifSchema is the SARIF 2.1.0 JSON schema URL.
const sarifSchema = "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json"

// sarifLog is the top-level SARIF 2.1.0 document.
type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool       sarifTool     `json:"tool"`
	Results    []sarifResult `json:"results"`
	ColumnKind string        `json:"columnKind"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name            string      `json:"name"`
	InformationURI  string      `json:"informationUri,omitempty"`
	SemanticVersion string      `json:"semanticVersion,omitempty"`
	Rules           []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string               `json:"id"`
	Name             string               `json:"name,omitempty"`
	ShortDescription sarifMultiformatMsg  `json:"shortDescription"`
	FullDescription  *sarifMultiformatMsg `json:"fullDescription,omitempty"`
	HelpURI          string               `json:"helpUri,omitempty"`
	Properties       map[string]any       `json:"properties,omitempty"`
}

type sarifMultiformatMsg struct {
	Text string `json:"text"`
}

type sarifResult struct {
	RuleID    string              `json:"ruleId"`
	Level     string              `json:"level"`
	Message   sarifMultiformatMsg `json:"message"`
	Locations []sarifLocation     `json:"locations,omitempty"`
}

type sarifLocation struct {
	PhysicalLocation *sarifPhysicalLocation `json:"physicalLocation,omitempty"`
	LogicalLocations []sarifLogicalLocation `json:"logicalLocations,omitempty"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           *sarifRegion          `json:"region,omitempty"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn"`
	EndLine     int `json:"endLine"`
	EndColumn   int `json:"endColumn"`
}

type sarifLogicalLocation struct {
	Name               string `json:"name,omitempty"`
	FullyQualifiedName string `json:"fullyQualifiedName,omitempty"`
	Kind               string `json:"kind,omitempty"`
}

// ruleDescriptions maps each issue code to its human-readable description,
// used to build the SARIF rule index.
var ruleDescriptions = map[IssueCode]string{
	CodeCrossModulePrivate:     "A module imports another module's private (non-API) package instead of its public API.",
	CodeUndeclaredDependency:   "A module depends on a module that is not declared in its allowed dependencies.",
	CodeForbiddenDependency:    "A module depends on a module that is explicitly forbidden.",
	CodeCycle:                  "Cyclic module dependency detected.",
	CodeOrphanPackage:          "A package is not part of any module.",
	CodeMissingPublicAPI:       "A module has no public API package.",
	CodeInvalidPublicAPI:       "A declared public API package does not exist or does not belong to the module.",
	CodeMissingPublishedEvent:  "A module declares a published event type that does not exist in the module.",
	CodeEventDrivenViolation:   "A module declared event-driven towards another module but imports a non-event package of it.",
	CodeEventPackageMissing:    "A module is event-driven towards a module that has no event packages.",
	CodeCrossModuleTypeLeakage: "A module's public surface exposes a type defined in another module's private packages.",
	CodeInternalAPITypeLeakage: "A module's public surface exposes a type defined in its own private packages.",
}

// ExportSARIF returns the verification findings as a SARIF 2.1.0 document,
// suitable for GitHub code scanning and other SARIF consumers.
func (a *Application) ExportSARIF() ([]byte, error) {
	res, err := a.Verify()
	if err != nil {
		return nil, err
	}

	// Build the rule index from the codes present in the result.
	codes := map[IssueCode]bool{}
	for _, i := range res.Issues {
		codes[i.Code] = true
	}
	ruleIDs := make([]string, 0, len(codes))
	for c := range codes {
		ruleIDs = append(ruleIDs, string(c))
	}
	sort.Strings(ruleIDs)

	log := sarifLog{
		Schema:  sarifSchema,
		Version: "2.1.0",
		Runs: []sarifRun{
			{
				ColumnKind: "utf16CodeUnits",
				Tool: sarifTool{
					Driver: sarifDriver{
						Name:            "gomodulith",
						InformationURI:  "https://github.com/MouXiaoJun/gomodulith",
						SemanticVersion: Version,
						Rules:           make([]sarifRule, 0, len(ruleIDs)),
					},
				},
				Results: make([]sarifResult, 0, len(res.Issues)),
			},
		},
	}

	for _, id := range ruleIDs {
		code := IssueCode(id)
		log.Runs[0].Tool.Driver.Rules = append(log.Runs[0].Tool.Driver.Rules, sarifRule{
			ID:               id,
			Name:             id,
			ShortDescription: sarifMultiformatMsg{Text: ruleDescriptions[code]},
			Properties:       map[string]any{"architecture": true},
		})
	}

	for _, issue := range res.Issues {
		level := "warning"
		if issue.Severity == SeverityError {
			level = "error"
		}
		r := sarifResult{
			RuleID:  string(issue.Code),
			Level:   level,
			Message: sarifMultiformatMsg{Text: issue.Message},
		}
		if loc := a.locationForIssue(issue); loc != nil {
			path := a.sourcePath(loc.File)
			uri := (&url.URL{Path: path}).String()
			if filepath.IsAbs(path) {
				uri = fileURI(path)
			}
			r.Locations = append(r.Locations, sarifLocation{
				PhysicalLocation: &sarifPhysicalLocation{
					ArtifactLocation: sarifArtifactLocation{URI: uri},
					Region: &sarifRegion{
						StartLine: loc.Range.Start.Line + 1, StartColumn: loc.Range.Start.Character + 1,
						EndLine: loc.Range.End.Line + 1, EndColumn: loc.Range.End.Character + 1,
					},
				},
			})
		} else if issue.Module != "" {
			r.Locations = append(r.Locations, sarifLocation{
				LogicalLocations: []sarifLogicalLocation{
					{Name: issue.Module, Kind: "module"},
				},
			})
		}
		log.Runs[0].Results = append(log.Runs[0].Results, r)
	}

	out, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("modulith: marshal sarif: %w", err)
	}
	return out, nil
}
