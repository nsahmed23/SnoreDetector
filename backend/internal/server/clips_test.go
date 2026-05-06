package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	stdlog "log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// wavBytes builds a minimal-but-realistic RIFF/WAVE header followed
// by `payload` PCM bytes. http.DetectContentType sniffs `audio/wave`
// for this header; we map that to the allowlist below.
func wavBytes(payload []byte) []byte {
	const sampleRate = 16000
	dataLen := uint32(len(payload))
	totalLen := dataLen + 36
	hdr := make([]byte, 0, 44)
	hdr = append(hdr, []byte("RIFF")...)
	hdr = append(hdr,
		byte(totalLen), byte(totalLen>>8), byte(totalLen>>16), byte(totalLen>>24),
	)
	hdr = append(hdr, []byte("WAVEfmt ")...)
	hdr = append(hdr, 16, 0, 0, 0)              // fmt chunk size
	hdr = append(hdr, 1, 0)                     // PCM
	hdr = append(hdr, 1, 0) // 1 channel
	sr := uint32(sampleRate)
	hdr = append(hdr, byte(sr), byte(sr>>8), byte(sr>>16), byte(sr>>24))
	br := uint32(sampleRate * 2)
	hdr = append(hdr, byte(br), byte(br>>8), byte(br>>16), byte(br>>24))
	hdr = append(hdr, 2, 0) // block align
	hdr = append(hdr, 16, 0)                    // bits per sample
	hdr = append(hdr, []byte("data")...)
	hdr = append(hdr,
		byte(dataLen), byte(dataLen>>8), byte(dataLen>>16), byte(dataLen>>24),
	)
	return append(hdr, payload...)
}

// buildClipMultipart returns (body, content-type) for a multipart
// upload with the given metadata + file body. Optional overrides let
// the test claim the wrong sha256, the wrong content-type, etc.
type clipUpload struct {
	clientClipID  string
	clientEventID string
	sessionID     string
	startedAt     time.Time
	durationMS    int32
	avgDB         *float32
	sha256Hex     string  // overrides the computed hash if non-empty
	fileBody      []byte
	filePartCT    string // overrides "audio/wav"
	skipMetaPart  bool
	skipFilePart  bool
	extraJSON     map[string]any
}

func defaultClipUpload() clipUpload {
	return clipUpload{
		clientClipID: uuid.NewString(),
		startedAt:    time.Date(2026, 5, 5, 3, 14, 0, 0, time.UTC),
		durationMS:   1500,
		fileBody:     wavBytes(bytes.Repeat([]byte{1, 2, 3, 4}, 64)),
		filePartCT:   "audio/wav",
	}
}

func (u clipUpload) build(t *testing.T) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if !u.skipMetaPart {
		// Compute the correct sha256 unless the test claims an override.
		sha := u.sha256Hex
		if sha == "" {
			h := sha256.Sum256(u.fileBody)
			sha = hex.EncodeToString(h[:])
		}
		meta := map[string]any{
			"client_clip_id": u.clientClipID,
			"started_at":     u.startedAt.UTC().Format(time.RFC3339Nano),
			"duration_ms":    u.durationMS,
			"sha256":         sha,
		}
		if u.clientEventID != "" {
			meta["client_event_id"] = u.clientEventID
		}
		if u.sessionID != "" {
			meta["session_id"] = u.sessionID
		}
		if u.avgDB != nil {
			meta["avg_db"] = *u.avgDB
		}
		for k, v := range u.extraJSON {
			meta[k] = v
		}
		mh := textproto.MIMEHeader{}
		mh.Set("Content-Disposition", `form-data; name="metadata"`)
		mh.Set("Content-Type", "application/json")
		w, err := mw.CreatePart(mh)
		if err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(meta)
	}
	if !u.skipFilePart {
		mh := textproto.MIMEHeader{}
		mh.Set("Content-Disposition", `form-data; name="file"; filename="clip.wav"`)
		mh.Set("Content-Type", u.filePartCT)
		w, err := mw.CreatePart(mh)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(u.fileBody); err != nil {
			t.Fatal(err)
		}
	}
	_ = mw.Close()
	return &buf, mw.FormDataContentType()
}

func postClip(t *testing.T, h *harness, tok string, u clipUpload) *httptest.ResponseRecorder {
	t.Helper()
	body, ct := u.build(t)
	req := httptest.NewRequest(http.MethodPost, "/audio/clips", body)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

// Wire the WAV-sniff allowlist for these tests. http.DetectContentType
// returns "audio/wave" for the RIFF header, so we add it explicitly.
func clipHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	// Re-build the router with audio/wave in the allowlist + a small
	// max-bytes for the "too large" test below.
	deps := Deps{
		Logger:            stdlog.New(stdlog.NewTextHandler(io.Discard, nil)),
		Store:             h.store,
		Apple:             h.apple,
		JWT:               h.jwt,
		Now:               func() time.Time { return time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC) },
		BlobStore:         h.blob,
		AudioMaxClipBytes: 4096,
		AudioAllowedMIME:  []string{"audio/wav", "audio/wave", "audio/m4a", "audio/mp4"},
	}
	h.router = New(deps)
	return h
}

func TestPostClip_HappyPath(t *testing.T) {
	h := clipHarness(t)
	uid := uuid.New()
	tok := h.sessionToken(t, uid)
	rec := postClip(t, h, tok, defaultClipUpload())
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp postClipResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Created {
		t.Errorf("created=false on first post")
	}
	if !strings.HasPrefix(resp.ObjectKey, "clips/"+uid.String()+"/") {
		t.Errorf("object_key shape: %q", resp.ObjectKey)
	}
	if !h.blob.Has(resp.ObjectKey) {
		t.Errorf("blobstore missing object %q", resp.ObjectKey)
	}
}

func TestPostClip_TooLarge(t *testing.T) {
	h := clipHarness(t) // cap = 4096 bytes
	tok := h.sessionToken(t, uuid.New())
	u := defaultClipUpload()
	u.fileBody = wavBytes(bytes.Repeat([]byte{0xAA}, 8192))
	rec := postClip(t, h, tok, u)
	if rec.Code != http.StatusRequestEntityTooLarge && rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d body=%s; want 413 or 400", rec.Code, rec.Body.String())
	}
}

func TestPostClip_WrongMIME(t *testing.T) {
	h := clipHarness(t)
	tok := h.sessionToken(t, uuid.New())
	u := defaultClipUpload()
	u.filePartCT = "audio/m4a"
	u.fileBody = []byte("this is plain text, not audio at all")
	rec := postClip(t, h, tok, u)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status=%d body=%s; want 415", rec.Code, rec.Body.String())
	}
}

func TestPostClip_SHA256Mismatch(t *testing.T) {
	h := clipHarness(t)
	tok := h.sessionToken(t, uuid.New())
	u := defaultClipUpload()
	// Claim a deliberately wrong (but well-formed) hash.
	u.sha256Hex = strings.Repeat("a", 64)
	rec := postClip(t, h, tok, u)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d body=%s; want 400", rec.Code, rec.Body.String())
	}
}

func TestPostClip_DuplicateClientClipID(t *testing.T) {
	h := clipHarness(t)
	uid := uuid.New()
	tok := h.sessionToken(t, uid)
	u := defaultClipUpload()
	u.clientClipID = "dup-clip"

	first := postClip(t, h, tok, u)
	if first.Code != http.StatusOK {
		t.Fatalf("first: status=%d body=%s", first.Code, first.Body.String())
	}
	var firstResp postClipResponse
	_ = json.Unmarshal(first.Body.Bytes(), &firstResp)
	firstBytes := h.blob.Bytes(firstResp.ObjectKey)
	if firstBytes == nil {
		t.Fatal("first put missing from blobstore")
	}

	// Replay with a *different* body but the same client_clip_id.
	// The server should return the original row + must NOT overwrite.
	u2 := u
	u2.fileBody = wavBytes(bytes.Repeat([]byte{0xFF}, 64))
	second := postClip(t, h, tok, u2)
	if second.Code != http.StatusOK {
		t.Fatalf("second: status=%d body=%s", second.Code, second.Body.String())
	}
	var secondResp postClipResponse
	_ = json.Unmarshal(second.Body.Bytes(), &secondResp)
	if secondResp.Created {
		t.Errorf("second POST returned created=true; idempotency broken")
	}
	if secondResp.ClipID != firstResp.ClipID {
		t.Errorf("clip_id changed: %q -> %q", firstResp.ClipID, secondResp.ClipID)
	}
	// Object key unchanged + bytes unchanged (no overwrite).
	if secondResp.ObjectKey != firstResp.ObjectKey {
		t.Errorf("object_key changed on duplicate: %q -> %q", firstResp.ObjectKey, secondResp.ObjectKey)
	}
	if !bytes.Equal(h.blob.Bytes(secondResp.ObjectKey), firstBytes) {
		t.Errorf("blobstore bytes changed on duplicate; idempotency broken")
	}
}

func TestPostClip_RequiresAuth(t *testing.T) {
	h := clipHarness(t)
	body, ct := defaultClipUpload().build(t)
	req := httptest.NewRequest(http.MethodPost, "/audio/clips", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status=%d, want 401", rec.Code)
	}
}

func TestListClips_OnlyOwn(t *testing.T) {
	h := clipHarness(t)
	mine := uuid.New()
	other := uuid.New()
	tok := h.sessionToken(t, mine)
	rec := postClip(t, h, tok, defaultClipUpload())
	if rec.Code != http.StatusOK {
		t.Fatalf("post mine: %d body=%s", rec.Code, rec.Body.String())
	}
	otherTok := h.sessionToken(t, other)
	rec = postClip(t, h, otherTok, defaultClipUpload())
	if rec.Code != http.StatusOK {
		t.Fatalf("post other: %d body=%s", rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/audio/clips", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec = httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d body=%s", rec.Code, rec.Body.String())
	}
	var resp listClipsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Clips) != 1 {
		t.Errorf("clips=%d, want 1 (cross-user leak?)", len(resp.Clips))
	}
}

func TestDownloadClip_ReturnsBody(t *testing.T) {
	h := clipHarness(t)
	uid := uuid.New()
	tok := h.sessionToken(t, uid)
	u := defaultClipUpload()
	rec := postClip(t, h, tok, u)
	if rec.Code != http.StatusOK {
		t.Fatalf("post: %d body=%s", rec.Code, rec.Body.String())
	}
	var resp postClipResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)

	req := httptest.NewRequest(http.MethodGet, "/audio/clips/"+resp.ClipID+"/download", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec = httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("download: %d body=%s", rec.Code, rec.Body.String())
	}
	got, _ := io.ReadAll(rec.Body)
	if !bytes.Equal(got, u.fileBody) {
		t.Errorf("download body mismatch: got %d bytes, want %d", len(got), len(u.fileBody))
	}
}

func TestDownloadClip_CrossUserIs404(t *testing.T) {
	h := clipHarness(t)
	mine := uuid.New()
	tok := h.sessionToken(t, mine)
	rec := postClip(t, h, tok, defaultClipUpload())
	if rec.Code != http.StatusOK {
		t.Fatalf("post: %d body=%s", rec.Code, rec.Body.String())
	}
	var resp postClipResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)

	other := h.sessionToken(t, uuid.New())
	req := httptest.NewRequest(http.MethodGet, "/audio/clips/"+resp.ClipID+"/download", nil)
	req.Header.Set("Authorization", "Bearer "+other)
	rec = httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("cross-user download: status=%d, want 404", rec.Code)
	}
}

func TestDeleteClip_HidesFromList(t *testing.T) {
	h := clipHarness(t)
	uid := uuid.New()
	tok := h.sessionToken(t, uid)
	rec := postClip(t, h, tok, defaultClipUpload())
	if rec.Code != http.StatusOK {
		t.Fatalf("post: %d body=%s", rec.Code, rec.Body.String())
	}
	var resp postClipResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)

	delReq := httptest.NewRequest(http.MethodDelete, "/audio/clips/"+resp.ClipID, nil)
	delReq.Header.Set("Authorization", "Bearer "+tok)
	delRec := httptest.NewRecorder()
	h.router.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete: %d body=%s", delRec.Code, delRec.Body.String())
	}

	// Object best-effort removed from the blobstore.
	if h.blob.Has(resp.ObjectKey) {
		t.Errorf("blobstore still has %q after delete", resp.ObjectKey)
	}

	// List no longer returns it.
	listReq := httptest.NewRequest(http.MethodGet, "/audio/clips", nil)
	listReq.Header.Set("Authorization", "Bearer "+tok)
	listRec := httptest.NewRecorder()
	h.router.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list: %d body=%s", listRec.Code, listRec.Body.String())
	}
	var listResp listClipsResponse
	_ = json.Unmarshal(listRec.Body.Bytes(), &listResp)
	if len(listResp.Clips) != 0 {
		t.Errorf("deleted clip still listed: %+v", listResp.Clips)
	}
}

func TestDeleteClip_NotFoundIs404(t *testing.T) {
	h := clipHarness(t)
	tok := h.sessionToken(t, uuid.New())
	req := httptest.NewRequest(http.MethodDelete, "/audio/clips/"+uuid.NewString(), nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", rec.Code)
	}
}

// --- export ?include= integration ---

func TestExport_IncludeClips_AddsAudioToTopLevel(t *testing.T) {
	// This is a server-side smoke test: we drive POST /audio/clips
	// here, but the export endpoints live on a separate router. So
	// instead we check that the fakeStore picked up the clip and the
	// existing export tests in internal/export will see the JSON
	// shape (covered there with ?include=clips).
	h := clipHarness(t)
	uid := uuid.New()
	tok := h.sessionToken(t, uid)
	rec := postClip(t, h, tok, defaultClipUpload())
	if rec.Code != http.StatusOK {
		t.Fatalf("post: %d body=%s", rec.Code, rec.Body.String())
	}
	if len(h.store.clips) != 1 {
		t.Errorf("store should have 1 clip; got %d", len(h.store.clips))
	}
}

// Ensure validateClipMetadata catches the obvious bad inputs without
// needing a full multipart envelope. Cheap unit-level coverage for
// the validator path.
func TestValidateClipMetadata(t *testing.T) {
	good := clipMetadataRequest{
		ClientClipID: "x",
		StartedAt:    time.Now(),
		DurationMS:   1000,
		SHA256:       strings.Repeat("0", 64),
	}
	cases := []struct {
		name string
		mut  func(*clipMetadataRequest)
		want bool // want pass
	}{
		{"happy", func(c *clipMetadataRequest) {}, true},
		{"missing id", func(c *clipMetadataRequest) { c.ClientClipID = "" }, false},
		{"id too long", func(c *clipMetadataRequest) { c.ClientClipID = strings.Repeat("a", 200) }, false},
		{"missing started_at", func(c *clipMetadataRequest) { c.StartedAt = time.Time{} }, false},
		{"negative duration", func(c *clipMetadataRequest) { c.DurationMS = -1 }, false},
		{"sha wrong length", func(c *clipMetadataRequest) { c.SHA256 = "abc" }, false},
		{"sha non-hex", func(c *clipMetadataRequest) { c.SHA256 = strings.Repeat("g", 64) }, false},
		{"avgdb out of range", func(c *clipMetadataRequest) { v := float32(300); c.AvgDB = &v }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := good
			tc.mut(&c)
			err := validateClipMetadata(c)
			if (err == nil) != tc.want {
				t.Errorf("err=%v, want pass=%v", err, tc.want)
			}
		})
	}
}

// Sanity-check the multipart helper itself so a bug in the test
// builder doesn't masquerade as a server-side failure.
func TestClipUploadHelper(t *testing.T) {
	body, ct := defaultClipUpload().build(t)
	if !strings.HasPrefix(ct, "multipart/form-data") {
		t.Errorf("content-type = %q", ct)
	}
	if body.Len() == 0 {
		t.Error("body is empty")
	}
	if !bytes.Contains(body.Bytes(), []byte(`"client_clip_id"`)) {
		t.Error("metadata part missing client_clip_id")
	}
}

