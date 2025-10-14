package ontology

// GetDefaultContext returns the standard JSON-LD context
func GetDefaultContext() map[string]interface{} {
	return map[string]interface{}{
		"@vocab": "https://saref.etsi.org/core#",
		"jeeves": "https://jeeves.home/vocab#",
		"adl":    "http://purl.org/adl#",
		"sosa":   "http://www.w3.org/ns/sosa/",
		"prov":   "http://www.w3.org/ns/prov#",
		"xsd":    "http://www.w3.org/2001/XMLSchema#",
	}
}
