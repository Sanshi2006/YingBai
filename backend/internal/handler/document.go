package handler

import (
	"errors"
	"mime/multipart"
	"net/http"

	"github.com/gin-gonic/gin"

	"project-for-yingbai/backend/internal/model"
	"project-for-yingbai/backend/internal/service"
)

const multipartOverheadAllowance int64 = 1 << 20

type DocumentHandler struct {
	documents *service.DocumentService
}

func NewDocumentHandler(documents *service.DocumentService) *DocumentHandler {
	return &DocumentHandler{documents: documents}
}

func (h *DocumentHandler) Upload(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, service.MaxDocumentSize+multipartOverheadAllowance)

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) || errors.Is(err, multipart.ErrMessageTooLarge) {
			writeDocumentError(c, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "文件不能超过 20 MB")
			return
		}
		if errors.Is(err, http.ErrMissingFile) {
			writeDocumentError(c, http.StatusBadRequest, "FILE_REQUIRED", "请选择需要上传的文件")
			return
		}

		writeDocumentError(c, http.StatusBadRequest, "INVALID_MULTIPART", "上传请求格式不正确")
		return
	}
	defer file.Close()

	metadata := model.DocumentMetadataInput{
		Category:   c.PostForm("category"),
		Type:       c.PostForm("type"),
		Permission: c.PostForm("permission"),
	}
	document, err := h.documents.Save(c.Request.Context(), file, header, metadata)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrDocumentRequired):
			writeDocumentError(c, http.StatusBadRequest, "FILE_REQUIRED", "请选择非空文件")
		case errors.Is(err, service.ErrDocumentTooLarge):
			writeDocumentError(c, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "文件不能超过 20 MB")
		case errors.Is(err, service.ErrUnsupportedDocumentType):
			writeDocumentError(c, http.StatusUnsupportedMediaType, "UNSUPPORTED_FILE_TYPE", "仅支持 PDF、TXT、MD 格式")
		case errors.Is(err, service.ErrInvalidCategory):
			writeDocumentError(c, http.StatusBadRequest, "INVALID_CATEGORY", "品类必须是：纺织、鞋类或杂货")
		case errors.Is(err, service.ErrInvalidType):
			writeDocumentError(c, http.StatusBadRequest, "INVALID_DOCUMENT_TYPE", "类型必须是：标准、业务规范或 FAQ")
		case errors.Is(err, service.ErrInvalidPermission):
			writeDocumentError(c, http.StatusBadRequest, "INVALID_PERMISSION", "权限必须是：公开或内部")
		case errors.Is(err, service.ErrExtractedTextEmpty):
			writeDocumentError(c, http.StatusUnprocessableEntity, "NO_EXTRACTABLE_TEXT", "文档中未提取到可用文本")
		case errors.Is(err, service.ErrExtractedTextTooLarge):
			writeDocumentError(c, http.StatusUnprocessableEntity, "EXTRACTED_TEXT_TOO_LARGE", "提取后的文本超过 10 MB")
		case errors.Is(err, service.ErrTextExtractionFailed):
			writeDocumentError(c, http.StatusUnprocessableEntity, "TEXT_EXTRACTION_FAILED", "文档文本提取失败")
		case errors.Is(err, service.ErrTextChunkingFailed):
			writeDocumentError(c, http.StatusUnprocessableEntity, "TEXT_CHUNKING_FAILED", "文档文本切割失败")
		case errors.Is(err, service.ErrEmbeddingFailed):
			writeDocumentError(c, http.StatusBadGateway, "EMBEDDING_FAILED", "文档向量化失败，请稍后重试")
		default:
			writeDocumentError(c, http.StatusInternalServerError, "UPLOAD_FAILED", "文件上传失败，请稍后重试")
		}
		return
	}

	c.JSON(http.StatusCreated, gin.H{"document": document})
}

func (h *DocumentHandler) List(c *gin.Context) {
	documents, err := h.documents.List(c.Request.Context())
	if err != nil {
		writeDocumentError(c, http.StatusInternalServerError, "DOCUMENT_LIST_FAILED", "文档列表查询失败，请稍后重试")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"documents": documents,
		"total":     len(documents),
	})
}

func writeDocumentError(c *gin.Context, status int, code, message string) {
	c.JSON(status, errorResponse{
		Error: errorBody{
			Code:    code,
			Message: message,
		},
	})
}
