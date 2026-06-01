package tui

const livePreviewDotHold = 3

var livePreviewDotFrames = []string{"·", "∙", "•", "●", "•", "∙"}

func livePreviewAnimationFrame(index int) string {
	if index < 0 {
		index = 0
	}
	if len(livePreviewDotFrames) == 0 {
		return ""
	}
	return livePreviewDotFrames[(index/livePreviewDotHold)%len(livePreviewDotFrames)]
}
