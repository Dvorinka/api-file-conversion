package converter

type JobInput struct {
	Filename     string `json:"filename"`
	TargetFormat string `json:"target_format"`
	FileBase64   string `json:"file_base64"`
}

type ConversionResult struct {
	Filename       string `json:"filename"`
	OutputFilename string `json:"output_filename,omitempty"`
	SourceFormat   string `json:"source_format,omitempty"`
	TargetFormat   string `json:"target_format,omitempty"`
	OutputMime     string `json:"output_mime,omitempty"`
	OutputBase64   string `json:"output_base64,omitempty"`
	SizeBytes      int    `json:"size_bytes,omitempty"`
	Error          string `json:"error,omitempty"`
}
