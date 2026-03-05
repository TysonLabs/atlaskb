package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/tgeorge06/atlaskb/internal/config"
	"github.com/tgeorge06/atlaskb/internal/db"
)

type setupStatusResponse struct {
	Configured bool          `json:"configured"`
	ConfigPath string        `json:"config_path"`
	Defaults   config.Config `json:"defaults"`
}

type setupApplyRequest struct {
	Config setupInputConfig `json:"config"`
}

type setupInputConfig struct {
	Database   setupInputDatabase   `json:"database"`
	LLM        setupInputLLM        `json:"llm"`
	Embeddings setupInputEmbeddings `json:"embeddings"`
	Pipeline   setupInputPipeline   `json:"pipeline"`
	Server     setupInputServer     `json:"server"`
	GitHub     setupInputGitHub     `json:"github"`
}

type setupInputDatabase struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	DBName   string `json:"dbname"`
	SSLMode  string `json:"sslmode"`
}

type setupInputLLM struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
}

type setupInputEmbeddings struct {
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
	APIKey  string `json:"api_key"`
}

type setupInputPipeline struct {
	Concurrency     int    `json:"concurrency"`
	ExtractionModel string `json:"extraction_model"`
	SynthesisModel  string `json:"synthesis_model"`
	ContextWindow   int    `json:"context_window"`
	GitLogLimit     int    `json:"git_log_limit"`
}

type setupInputServer struct {
	Port     int    `json:"port"`
	ChatsDir string `json:"chats_dir"`
}

type setupInputGitHub struct {
	Token          string `json:"token"`
	APIURL         string `json:"api_url"`
	MaxPRs         int    `json:"max_prs"`
	PRBatchSize    int    `json:"pr_batch_size"`
	EnterpriseHost string `json:"enterprise_host"`
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, setupStatusResponse{
		Configured: false,
		ConfigPath: s.configPath,
		Defaults:   s.cfg,
	})
}

func (s *Server) handleSetupApply(w http.ResponseWriter, r *http.Request) {
	var req setupApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, NewBadRequest("invalid setup payload"))
		return
	}

	cfg := mergeSetupConfig(req.Config)
	if err := config.Validate(cfg); err != nil {
		writeError(w, NewBadRequest("invalid configuration: "+err.Error()))
		return
	}

	testPool, err := db.Connect(r.Context(), cfg.Database)
	if err != nil {
		writeError(w, NewBadRequest("database connection failed: "+err.Error()))
		return
	}
	testPool.Close()

	if err := db.RunMigrations(cfg.Database.DSN()); err != nil {
		writeError(w, NewInternal("running migrations: "+err.Error()))
		return
	}

	if !checkHTTPDependency(r.Context(), cfg.LLM.BaseURL+"/v1/models", http.MethodGet, nil) {
		writeError(w, NewBadRequest("LLM endpoint check failed"))
		return
	}
	payload := []byte(`{"input":["health"],"model":"` + cfg.Embeddings.Model + `"}`)
	if !checkHTTPDependency(r.Context(), cfg.Embeddings.BaseURL+"/v1/embeddings", http.MethodPost, payload) {
		writeError(w, NewBadRequest("embeddings endpoint check failed"))
		return
	}

	if err := config.Save(cfg, s.configPath); err != nil {
		writeError(w, NewInternal("saving config: "+err.Error()))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":           "ok",
		"message":          "setup completed, restart runtime to enter normal mode",
		"restart_required": true,
	})
}

func mergeSetupConfig(in setupInputConfig) config.Config {
	cfg := config.DefaultConfig()

	cfg.Database.Host = strings.TrimSpace(in.Database.Host)
	cfg.Database.Port = in.Database.Port
	cfg.Database.User = strings.TrimSpace(in.Database.User)
	cfg.Database.Password = in.Database.Password
	cfg.Database.DBName = strings.TrimSpace(in.Database.DBName)
	cfg.Database.SSLMode = strings.TrimSpace(in.Database.SSLMode)

	cfg.LLM.BaseURL = strings.TrimSpace(in.LLM.BaseURL)
	cfg.LLM.APIKey = strings.TrimSpace(in.LLM.APIKey)

	cfg.Embeddings.BaseURL = strings.TrimSpace(in.Embeddings.BaseURL)
	cfg.Embeddings.Model = strings.TrimSpace(in.Embeddings.Model)
	cfg.Embeddings.APIKey = strings.TrimSpace(in.Embeddings.APIKey)

	if in.Pipeline.Concurrency > 0 {
		cfg.Pipeline.Concurrency = in.Pipeline.Concurrency
	}
	if v := strings.TrimSpace(in.Pipeline.ExtractionModel); v != "" {
		cfg.Pipeline.ExtractionModel = v
	}
	if v := strings.TrimSpace(in.Pipeline.SynthesisModel); v != "" {
		cfg.Pipeline.SynthesisModel = v
	}
	if in.Pipeline.ContextWindow > 0 {
		cfg.Pipeline.ContextWindow = in.Pipeline.ContextWindow
	}
	if in.Pipeline.GitLogLimit > 0 {
		cfg.Pipeline.GitLogLimit = in.Pipeline.GitLogLimit
	}

	if in.Server.Port > 0 {
		cfg.Server.Port = in.Server.Port
	}
	cfg.Server.ChatsDir = strings.TrimSpace(in.Server.ChatsDir)

	cfg.GitHub.Token = strings.TrimSpace(in.GitHub.Token)
	if v := strings.TrimSpace(in.GitHub.APIURL); v != "" {
		cfg.GitHub.APIURL = v
	}
	if in.GitHub.MaxPRs > 0 {
		cfg.GitHub.MaxPRs = in.GitHub.MaxPRs
	}
	if in.GitHub.PRBatchSize > 0 {
		cfg.GitHub.PRBatchSize = in.GitHub.PRBatchSize
	}
	cfg.GitHub.EnterpriseHost = strings.TrimSpace(in.GitHub.EnterpriseHost)

	// Final trim/default cleanup.
	if cfg.Database.Port == 0 {
		cfg.Database.Port = 5432
	}
	if cfg.Database.SSLMode == "" {
		cfg.Database.SSLMode = "disable"
	}
	return cfg
}

func (s *Server) handleSetupPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page := strings.ReplaceAll(setupPageHTMLTemplate, "{{CONFIG_PATH}}", s.configPath)
	_, _ = w.Write([]byte(page))
}

var setupPageHTMLTemplate = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>AtlasKB Setup</title>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; margin: 0; background: #f6f7fb; color: #111827; }
    .wrap { max-width: 860px; margin: 24px auto; padding: 0 16px; }
    .card { background: #fff; border: 1px solid #e5e7eb; border-radius: 12px; padding: 18px; margin-bottom: 14px; }
    h1 { margin: 0 0 8px; font-size: 28px; }
    h2 { margin: 0 0 12px; font-size: 18px; }
    p { margin: 6px 0 0; color: #4b5563; }
    .grid { display: grid; grid-template-columns: repeat(2, minmax(0,1fr)); gap: 10px; }
    .full { grid-column: 1 / -1; }
    label { display: flex; flex-direction: column; font-size: 13px; gap: 6px; color: #374151; }
    input { border: 1px solid #d1d5db; border-radius: 8px; padding: 10px; font-size: 14px; }
    button { border: 0; border-radius: 10px; padding: 12px 16px; font-weight: 600; background: #111827; color: #fff; cursor: pointer; }
    button:disabled { opacity: 0.6; cursor: default; }
    .msg { margin-top: 10px; font-size: 14px; }
    .ok { color: #065f46; }
    .err { color: #991b1b; white-space: pre-wrap; }
    .hint { font-size: 12px; color: #6b7280; margin-top: 8px; }
    @media (max-width: 720px) { .grid { grid-template-columns: 1fr; } }
  </style>
</head>
<body>
  <div class="wrap">
    <h1>AtlasKB Setup</h1>
    <p>Complete initial configuration, then restart runtime.</p>
    <div id="msg" class="msg"></div>

    <div class="card">
      <h2>Database</h2>
      <div class="grid">
        <label>Host<input id="db_host" value="db" /></label>
        <label>Port<input id="db_port" type="number" value="5432" /></label>
        <label>User<input id="db_user" value="atlaskb" /></label>
        <label>Password<input id="db_password" value="atlaskb" /></label>
        <label>Database<input id="db_name" value="atlaskb" /></label>
        <label>SSL Mode<input id="db_sslmode" value="disable" /></label>
      </div>
    </div>

    <div class="card">
      <h2>LLM + Pipeline</h2>
      <div class="grid">
        <label class="full">LLM Endpoint<input id="llm_url" value="http://host.docker.internal:1234" /></label>
        <label>LLM API Key (optional)<input id="llm_key" value="" /></label>
        <label>Extraction Model<input id="extract_model" value="qwen/qwen3.5-35b-a3b" /></label>
        <label>Synthesis Model<input id="synth_model" value="qwen/qwen3.5-35b-a3b" /></label>
        <label>Concurrency<input id="concurrency" type="number" value="2" /></label>
      </div>
    </div>

    <div class="card">
      <h2>Embeddings</h2>
      <div class="grid">
        <label class="full">Embeddings Endpoint<input id="emb_url" value="http://host.docker.internal:1234" /></label>
        <label>Embeddings Model<input id="emb_model" value="mxbai-embed-large-v1" /></label>
        <label>Embeddings API Key (optional)<input id="emb_key" value="" /></label>
      </div>
    </div>

    <div class="card">
      <h2>Runtime + GitHub</h2>
      <div class="grid">
        <label>Server Port<input id="server_port" type="number" value="3000" /></label>
        <label>GitHub Token (optional)<input id="gh_token" value="" /></label>
      </div>
      <div class="hint">Config path: {{CONFIG_PATH}}</div>
    </div>

    <button id="saveBtn">Save and Validate Setup</button>
    <div class="hint">After success: <code>docker compose restart atlaskb</code></div>
  </div>

  <script>
    const msg = document.getElementById("msg");
    const btn = document.getElementById("saveBtn");

    function setMsg(text, cls) {
      msg.className = "msg " + cls;
      msg.textContent = text;
    }

    async function applySetup() {
      btn.disabled = true;
      setMsg("Applying setup...", "");
      const payload = {
        config: {
          database: {
            host: document.getElementById("db_host").value.trim(),
            port: Number(document.getElementById("db_port").value || 5432),
            user: document.getElementById("db_user").value.trim(),
            password: document.getElementById("db_password").value,
            dbname: document.getElementById("db_name").value.trim(),
            sslmode: document.getElementById("db_sslmode").value.trim()
          },
          llm: {
            base_url: document.getElementById("llm_url").value.trim(),
            api_key: document.getElementById("llm_key").value.trim()
          },
          embeddings: {
            base_url: document.getElementById("emb_url").value.trim(),
            model: document.getElementById("emb_model").value.trim(),
            api_key: document.getElementById("emb_key").value.trim()
          },
          pipeline: {
            extraction_model: document.getElementById("extract_model").value.trim(),
            synthesis_model: document.getElementById("synth_model").value.trim(),
            concurrency: Number(document.getElementById("concurrency").value || 2)
          },
          server: {
            port: Number(document.getElementById("server_port").value || 3000)
          },
          github: {
            token: document.getElementById("gh_token").value.trim()
          }
        }
      };

      try {
        const res = await fetch("/api/setup/apply", {
          method: "POST",
          headers: {"Content-Type": "application/json"},
          body: JSON.stringify(payload)
        });
        const data = await res.json();
        if (!res.ok) {
          throw new Error(data.error || "setup failed");
        }
        setMsg("Setup saved. Restart runtime now: docker compose restart atlaskb", "ok");
      } catch (err) {
        setMsg(String(err), "err");
      } finally {
        btn.disabled = false;
      }
    }

    btn.addEventListener("click", applySetup);
  </script>
</body>
</html>`
