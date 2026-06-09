package toolset

//
// Schemas

var (
	listDatasetsSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prefix": map[string]any{
				"type":        "string",
				"description": "Filter datasets by name prefix.",
			},
			"page_size": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     512,
				"description": "Maximum number of datasets to return.",
			},
			"page_token": map[string]any{
				"type":        "string",
				"description": "Pagination token from previous response.",
			},
		},
		"additionalProperties": false,
	}

	listFilesSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dataset_id": map[string]any{
				"type":        "string",
				"description": "Dataset ID (use this OR dataset_name).",
			},
			"dataset_name": map[string]any{
				"type":        "string",
				"description": "Dataset name (use this OR dataset_id).",
			},
			"latest": map[string]any{
				"type":        "boolean",
				"description": "When using dataset_name, select the latest READY version.",
			},
			"glob_pattern": map[string]any{
				"type":        "string",
				"description": "Glob pattern to filter files (e.g., '*.txt', 'data/**/*.json').",
			},
			"path_prefix": map[string]any{
				"type":        "string",
				"description": "Path prefix to filter files.",
			},
			"page_size": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     512,
				"description": "Maximum number of files to return.",
			},
			"page_token": map[string]any{
				"type":        "string",
				"description": "Pagination token from previous response.",
			},
		},
		"anyOf": []any{
			map[string]any{"required": []string{"dataset_id"}},
			map[string]any{"required": []string{"dataset_name"}},
		},
		"additionalProperties": false,
	}

	readFileSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dataset_id": map[string]any{
				"type":        "string",
				"description": "Dataset ID (use this OR dataset_name).",
			},
			"dataset_name": map[string]any{
				"type":        "string",
				"description": "Dataset name (use this OR dataset_id).",
			},
			"latest": map[string]any{
				"type":        "boolean",
				"description": "When using dataset_name, select the latest READY version.",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the file within the dataset.",
			},
			"offset": map[string]any{
				"type":        "integer",
				"minimum":     0,
				"default":     0,
				"description": "Byte offset to start reading from.",
			},
			"length": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     maxReadLength,
				"default":     defaultReadLength,
				"description": "Number of bytes to return (default 250, max 2000).",
			},
		},
		"required": []string{"path"},
		"anyOf": []any{
			map[string]any{"required": []string{"dataset_id"}},
			map[string]any{"required": []string{"dataset_name"}},
		},
		"additionalProperties": false,
	}

	getDownloadURLSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dataset_id": map[string]any{
				"type":        "string",
				"description": "Dataset ID (use this OR dataset_name).",
			},
			"dataset_name": map[string]any{
				"type":        "string",
				"description": "Dataset name (use this OR dataset_id).",
			},
			"latest": map[string]any{
				"type":        "boolean",
				"description": "When using dataset_name, select the latest READY version.",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the file within the dataset.",
			},
		},
		"required": []string{"path"},
		"anyOf": []any{
			map[string]any{"required": []string{"dataset_id"}},
			map[string]any{"required": []string{"dataset_name"}},
		},
		"additionalProperties": false,
	}

	putFileSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dataset_name": map[string]any{
				"type":        "string",
				"description": "Name for the dataset.",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Path within the dataset for the file.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Text content to write to the file.",
			},
		},
		"required":             []string{"dataset_name", "path", "content"},
		"additionalProperties": false,
	}

	deleteDatasetSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dataset_id": map[string]any{
				"type":        "string",
				"description": "ID of the dataset to delete.",
			},
		},
		"required":             []string{"dataset_id"},
		"additionalProperties": false,
	}
)
