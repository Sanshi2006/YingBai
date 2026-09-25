package handler

import (
	"errors"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"project-for-yingbai/backend/internal/model"
	"project-for-yingbai/backend/internal/service"
)

const multipartOverheadAllowance int64 = 1 << 20

type DocumentHandler struct {
	documents *service.DocumentService
}

type documentErrorBody struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	DocumentID string `json:"documentId,omitempty"`
}

type documentErrorResponse struct {
	Error documentErrorBody `json:"error"`
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
		writeDocumentServiceError(c, err, document.ID)
		return
	}

	c.JSON(http.StatusCreated, gin.H{"document": document})
}

func (h *DocumentHandler) List(c *gin.Context) {
	filter, err := documentListFilter(c)
	if err != nil {
		writeDocumentError(c, http.StatusBadRequest, "INVALID_DOCUMENT_FILTER", "分页或筛选条件不正确")
		return
	}
	result, err := h.documents.List(c.Request.Context(), filter)
	if err != nil {
		if errors.Is(err, service.ErrInvalidDocumentFilter) {
			writeDocumentError(c, http.StatusBadRequest, "INVALID_DOCUMENT_FILTER", "分页或筛选条件不正确")
			return
		}
		writeDocumentError(c, http.StatusInternalServerError, "DOCUMENT_LIST_FAILED", "文档列表查询失败，请稍后重试")
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *DocumentHandler) Delete(c *gin.Context) {
	if err := h.documents.Delete(c.Request.Context(), c.Param("id")); err != nil {
		if errors.Is(err, service.ErrDocumentNotFound) {
			writeDocumentError(c, http.StatusNotFound, "DOCUMENT_NOT_FOUND", "文档不存在")
			return
		}
		writeDocumentError(c, http.StatusInternalServerError, "DOCUMENT_DELETE_FAILED", "文档删除失败，请稍后重试")
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true, "id": c.Param("id")})
}

func (h *DocumentHandler) Retry(c *gin.Context) {
	document, err := h.documents.Retry(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeDocumentServiceError(c, err, document.ID)
		return
	}
	c.JSON(http.StatusOK, gin.H{"document": document})
}

func (h *DocumentHandler) Revectorize(c *gin.Context) {
	document, err := h.documents.Revectorize(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeDocumentServiceError(c, err, document.ID)
		return
	}
	c.JSON(http.StatusOK, gin.H{"document": document})
}

func writeDocumentError(c *gin.Context, status int, code, message string) {
	writeDocumentErrorWithID(c, status, code, message, "")
}

func writeDocumentErrorWithID(c *gin.Context, status int, code, message, documentID string) {
	c.JSON(status, documentErrorResponse{
		Error: documentErrorBody{
			Code:       code,
			Message:    message,
			DocumentID: documentID,
		},
	})
}

func writeDocumentServiceError(c *gin.Context, err error, documentID string) {
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
	case errors.Is(err, service.ErrDuplicateDocument):
		writeDocumentErrorWithID(c, http.StatusConflict, "DUPLICATE_DOCUMENT", "相同内容的文档已经存在", documentID)
	case errors.Is(err, service.ErrDocumentNotFound):
		writeDocumentError(c, http.StatusNotFound, "DOCUMENT_NOT_FOUND", "文档不存在")
	case errors.Is(err, service.ErrDocumentNotRetryable):
		writeDocumentErrorWithID(c, http.StatusConflict, "DOCUMENT_NOT_RETRYABLE", "只有处理失败的文档可以重试", documentID)
	case errors.Is(err, service.ErrExtractedTextEmpty):
		writeDocumentErrorWithID(c, http.StatusUnprocessableEntity, "NO_EXTRACTABLE_TEXT", "文档中未提取到可用文本，已保留失败记录", documentID)
	case errors.Is(err, service.ErrExtractedTextTooLarge):
		writeDocumentErrorWithID(c, http.StatusUnprocessableEntity, "EXTRACTED_TEXT_TOO_LARGE", "提取后的文本超过 10 MB，已保留失败记录", documentID)
	case errors.Is(err, service.ErrTextExtractionFailed):
		writeDocumentErrorWithID(c, http.StatusUnprocessableEntity, "TEXT_EXTRACTION_FAILED", "文档文本提取失败，已保留失败记录", documentID)
	case errors.Is(err, service.ErrTextChunkingFailed):
		writeDocumentErrorWithID(c, http.StatusUnprocessableEntity, "TEXT_CHUNKING_FAILED", "文档文本切割失败，已保留失败记录", documentID)
	case errors.Is(err, service.ErrEmbeddingFailed):
		writeDocumentErrorWithID(c, http.StatusBadGateway, "EMBEDDING_FAILED", "文档向量化失败，已保留失败记录，可稍后重试", documentID)
	default:
		writeDocumentErrorWithID(c, http.StatusInternalServerError, "DOCUMENT_PROCESSING_FAILED", "文档处理失败，请稍后重试", documentID)
	}
}

func documentListFilter(c *gin.Context) (model.DocumentListFilter, error) {
	page, err := optionalPositiveInteger(c.Query("page"))
	if err != nil {
		return model.DocumentListFilter{}, err
	}
	pageSize, err := optionalPositiveInteger(c.Query("pageSize"))
	if err != nil {
		return model.DocumentListFilter{}, err
	}
	return model.DocumentListFilter{
		Page:       page,
		PageSize:   pageSize,
		Query:      strings.TrimSpace(c.Query("q")),
		Category:   strings.TrimSpace(c.Query("category")),
		Type:       strings.TrimSpace(c.Query("type")),
		Permission: strings.TrimSpace(c.Query("permission")),
		Status:     strings.TrimSpace(c.Query("status")),
	}, nil
}

func optionalPositiveInteger(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return 0, service.ErrInvalidDocumentFilter
	}
	return parsed, nil
}
