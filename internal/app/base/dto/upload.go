package dto

type SignImageUploadRequest struct {
	Filename    string `json:"filename"     binding:"required,min=1,max=255"`
	ContentType string `json:"content_type" binding:"required,min=1,max=128"`
	Size        int64  `json:"size"         binding:"required,min=1"`
	Usage       string `json:"usage"        binding:"required,oneof=item avatar general"`
}

type SignImageUploadInput struct {
	Filename    string
	ContentType string
	Size        int64
	Usage       string
}

func (r SignImageUploadRequest) Input() SignImageUploadInput {
	return SignImageUploadInput{
		Filename:    r.Filename,
		ContentType: r.ContentType,
		Size:        r.Size,
		Usage:       r.Usage,
	}
}

type SignImageUploadResult struct {
	UploadURL  string            `json:"upload_url"`
	FormFields map[string]string `json:"form_fields"`
	ImageURL   string            `json:"image_url"`
	ObjectKey  string            `json:"object_key"`
	ExpiresIn  int64             `json:"expires_in"`
}
