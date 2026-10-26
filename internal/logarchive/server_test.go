package logarchive

import (
	"bytes"
	"encoding/json"
	"github.com/DATA-DOG/go-sqlmock"
	"net/http/httptest"
	"testing"
	"time"
)

func TestInternalLogRolesCannotImpersonateEachOther(t *testing.T) {
	now := time.Now()
	s := openTest(t, &now)
	o := &Observer{Store: s}
	req := collectorRequest(Input{})
	req.URL.Path = "/v1/read"
	w := httptest.NewRecorder()
	o.Handler().ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("collector read evidence with HTTP%d", w.Code)
	}
	req = collectorRequest(Input{})
	req.TLS.PeerCertificates[0].Subject.CommonName = "orbitjob-log-reader"
	w = httptest.NewRecorder()
	o.Handler().ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("reader ingested evidence with HTTP%d", w.Code)
	}
}

func TestDirectoryTombstoneSurvivesLocalReap(t *testing.T) {
	for _, state := range []string{"expired", "deleted", "capacity"} {
		t.Run(state, func(t *testing.T) {
			now := time.Now()
			s := openTest(t, &now)
			db, mock, e := sqlmock.New()
			if e != nil {
				t.Fatal(e)
			}
			defer func() { _ = db.Close() }()
			id := identity(81)
			mock.ExpectBegin()
			mock.ExpectExec("SELECT set_config").WithArgs(id.TenantID).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery("SELECT state,protected_until").WithArgs(id.TenantID, id.Key()).WillReturnRows(sqlmock.NewRows([]string{"state", "protected_until"}).AddRow(state, now))
			mock.ExpectCommit()
			o := &Observer{Store: s, Repository: Repository{DB: db}}
			body, _ := json.Marshal(ReadRequest{Identity: id})
			req := collectorRequest(Input{})
			req.URL.Path = "/v1/read"
			req.TLS.PeerCertificates[0].Subject.CommonName = "orbitjob-log-reader"
			req.Body = ioAuditBody(bytes.NewBuffer(body))
			w := httptest.NewRecorder()
			o.Handler().ServeHTTP(w, req)
			var page Page
			if e = json.Unmarshal(w.Body.Bytes(), &page); e != nil || w.Code != 200 || page.State != state {
				t.Fatalf("lost %s reason: HTTP%d %+v %v", state, w.Code, page, e)
			}
			if e = mock.ExpectationsWereMet(); e != nil {
				t.Fatal(e)
			}
		})
	}
}
