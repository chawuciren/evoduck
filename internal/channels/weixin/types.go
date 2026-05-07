package weixin

type WeixinMessage struct {
	Seq          int64         `json:"seq,omitempty"`
	MessageID    int64         `json:"message_id,omitempty"`
	FromUserID   string        `json:"from_user_id,omitempty"`
	ToUserID     string        `json:"to_user_id,omitempty"`
	CreateTimeMS int64         `json:"create_time_ms,omitempty"`
	SessionID    string        `json:"session_id,omitempty"`
	MessageType  int           `json:"message_type,omitempty"`
	MessageState int           `json:"message_state,omitempty"`
	ItemList     []MessageItem `json:"item_list,omitempty"`
	ContextToken string        `json:"context_token,omitempty"`
}

type MessageItem struct {
	Type      int         `json:"type"`
	TextItem  *TextItem   `json:"text_item,omitempty"`
	ImageItem *ImageItem  `json:"image_item,omitempty"`
	VoiceItem *VoiceItem  `json:"voice_item,omitempty"`
	FileItem  *FileItem   `json:"file_item,omitempty"`
	VideoItem *VideoItem  `json:"video_item,omitempty"`
	RefMsg    *RefMessage `json:"ref_msg,omitempty"`
}

type TextItem struct {
	Text string `json:"text"`
}

type CDNMedia struct {
	EncryptQueryParam string `json:"encrypt_query_param,omitempty"`
	AESKey            string `json:"aes_key,omitempty"`
	EncryptType       int    `json:"encrypt_type,omitempty"`
	FullURL           string `json:"full_url,omitempty"`
}

type ImageItem struct {
	Media      *CDNMedia `json:"media,omitempty"`
	ThumbMedia *CDNMedia `json:"thumb_media,omitempty"`
	AESKey     string    `json:"aeskey,omitempty"`
	URL        string    `json:"url,omitempty"`
	MidSize    int64     `json:"mid_size,omitempty"`
	ThumbSize  int64     `json:"thumb_size,omitempty"`
	HDSize     int64     `json:"hd_size,omitempty"`
}

type VoiceItem struct {
	Media      *CDNMedia `json:"media,omitempty"`
	EncodeType int       `json:"encode_type,omitempty"`
	SampleRate int       `json:"sample_rate,omitempty"`
	Playtime   int       `json:"playtime,omitempty"`
	Text       string    `json:"text,omitempty"`
}

type FileItem struct {
	Media    *CDNMedia `json:"media,omitempty"`
	FileName string    `json:"file_name,omitempty"`
	MD5      string    `json:"md5,omitempty"`
	Len      string    `json:"len,omitempty"`
}

type VideoItem struct {
	Media      *CDNMedia `json:"media,omitempty"`
	VideoSize  int64     `json:"video_size,omitempty"`
	PlayLength int       `json:"play_length,omitempty"`
	VideoMD5   string    `json:"video_md5,omitempty"`
	ThumbMedia *CDNMedia `json:"thumb_media,omitempty"`
	ThumbSize  int64     `json:"thumb_size,omitempty"`
}

type RefMessage struct {
	MessageID int64 `json:"message_id,omitempty"`
}

type GetUpdatesRequest struct {
	GetUpdatesBuf string `json:"get_updates_buf"`
}

type GetUpdatesResponse struct {
	Ret                  int             `json:"ret"`
	ErrCode              int             `json:"errcode,omitempty"`
	ErrMsg               string          `json:"errmsg,omitempty"`
	Msgs                 []WeixinMessage `json:"msgs,omitempty"`
	GetUpdatesBuf        string          `json:"get_updates_buf"`
	LongPollingTimeoutMS int             `json:"longpolling_timeout_ms,omitempty"`
}

type SendMessageRequest struct {
	Msg SendMessageMsg `json:"msg"`
}

type SendMessageMsg struct {
	ToUserID     string        `json:"to_user_id"`
	ContextToken string        `json:"context_token,omitempty"`
	ItemList     []MessageItem `json:"item_list"`
}

type GetUploadURLRequest struct {
	FileKey         string `json:"filekey"`
	MediaType       int    `json:"media_type"`
	ToUserID        string `json:"to_user_id"`
	RawSize         int64  `json:"rawsize"`
	RawFileMD5      string `json:"rawfilemd5"`
	FileSize        int64  `json:"filesize"`
	ThumbRawSize    int64  `json:"thumb_rawsize,omitempty"`
	ThumbRawFileMD5 string `json:"thumb_rawfilemd5,omitempty"`
	ThumbFileSize   int64  `json:"thumb_filesize,omitempty"`
	NoNeedThumb     bool   `json:"no_need_thumb,omitempty"`
	AESKey          string `json:"aeskey,omitempty"`
}

type GetUploadURLResponse struct {
	UploadParam      string `json:"upload_param,omitempty"`
	ThumbUploadParam string `json:"thumb_upload_param,omitempty"`
	UploadFullURL    string `json:"upload_full_url,omitempty"`
}

type GetConfigRequest struct {
	ILinkUserID  string `json:"ilink_user_id"`
	ContextToken string `json:"context_token,omitempty"`
}

type GetConfigResponse struct {
	Ret          int    `json:"ret"`
	TypingTicket string `json:"typing_ticket,omitempty"`
}

type SendTypingRequest struct {
	ILinkUserID  string `json:"ilink_user_id"`
	TypingTicket string `json:"typing_ticket"`
	Status       int    `json:"status"`
}
