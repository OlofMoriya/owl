package tools

import (
	"fmt"
	commontypes "owl/common_types"
	"owl/data"
	"owl/logger"

	"github.com/fatih/color"
)

var MODELNAME = "generate_image_tool"

type GenerateImageTool struct {
	ResponseHandler   commontypes.ResponseHandler
	HistoryRepository *data.HistoryRepository
	Context           *data.Context
}

type GenerateImageInput struct {
	Prompt string
}

func (tool *GenerateImageTool) SetHistory(repo *data.HistoryRepository, context *data.Context) {
	tool.Context = context
	tool.HistoryRepository = repo
}

func (tool *GenerateImageTool) Run(i map[string]string) (string, error) {
	prompt, exists := i["Prompt"]
	if !exists {
		return "", fmt.Errorf("Could not parse FileWriteInput from input")
	}

	logger.Screen(fmt.Sprintf("Asked to generate image with prompt: %v", prompt), color.RGB(150, 150, 150))

	return "Image generation tool is temporarily unavailable while the model integration is being refactored.", nil
}

func (tool *GenerateImageTool) GetName() string {
	return "image_generator"
}

func (tool *GenerateImageTool) GetDefinition() (Tool, string) {
	return Tool{
		Name:         tool.GetName(),
		Description:  "Generates images from a prompt. This call takes time so don't generate more than two at the time. Can take a subject and a style and creates a image which it returns as base64 and saves it to disc as png",
		Groups:       []ToolGroup{ToolGroupDeveloper},
		Dependencies: []ToolDependency{ToolDependencyLocalExec},

		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"Prompt": {
					Type:        "string",
					Description: "Promt with description of the image that should be created",
				},
			},
		},
	}, LOCAL
}

func (tool *GenerateImageTool) GetGroups() []ToolGroup {
	return []ToolGroup{ToolGroupDeveloper}
}

func init() {
	//todo: need the context?
	Register(&GenerateImageTool{})
}
