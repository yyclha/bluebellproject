package models

type ParamRAGSearch struct {
	Query string `json:"query" form:"query" binding:"required"`
	TopK  int    `json:"top_k" form:"top_k"`
}

type ParamRAGReindex struct {
	Limit int `json:"limit"`
}

type RAGHit struct {
	PostID      int64   `json:"post_id,string"`
	Score       float32 `json:"score"`
	Title       string  `json:"title"`
	Content     string  `json:"content"`
	ChunkIndex  int64   `json:"chunk_index"`
	ChunkText   string  `json:"chunk_text"`
	CommunityID int64   `json:"community_id"`
	AuthorID    int64   `json:"author_id"`
}

type RAGSearchResult struct {
	Query string   `json:"query"`
	Hits  []RAGHit `json:"hits"`
}
