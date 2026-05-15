package admin

import (
	"encoding/json"
	"net/http"

	"gateway-proxy/config"
	"gateway-proxy/db"
	"gateway-proxy/provider"
)

// HandleStats 处理 Admin 统计请求
func HandleStats(w http.ResponseWriter, r *http.Request) {
	authKey := provider.ExtractAPIKey(r)
	legacyQueryKey := r.URL.Query().Get("key")

	if authKey == "" && legacyQueryKey == config.Global.AdminKey {
		authKey = legacyQueryKey
		legacyQueryKey = ""
	}

	if !config.Global.IsAdminKey(authKey) {
		http.Error(w, `{"error":"Admin key required"}`, http.StatusUnauthorized)
		return
	}

	keyAlias := r.URL.Query().Get("key_alias")
	if keyAlias == "" {
		keyAlias = r.URL.Query().Get("alias")
	}
	if keyAlias == "" {
		keyAlias = legacyQueryKey
	}

	prov := r.URL.Query().Get("provider")
	days := 7
	if v := r.URL.Query().Get("days"); v != "" {
		if n := db.AtoiSafe(v); n > 0 {
			days = n
		}
	}

	stats, err := db.GetStats(keyAlias, prov, days)
	if err != nil {
		http.Error(w, "Query failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// HandleUI 管理页面
func HandleUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(adminUIHTML))
}

const adminUIHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>OmniProxy Admin</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:#0f1117;color:#e4e4e7;min-height:100vh}
.container{max-width:960px;margin:0 auto;padding:24px 20px}
h1{font-size:1.5rem;font-weight:600;margin-bottom:20px;color:#fff}
h1 span{color:#7c8aff}
.toolbar{display:flex;gap:12px;align-items:center;flex-wrap:wrap;margin-bottom:24px}
.toolbar input,.toolbar select{background:#1a1b26;border:1px solid #2a2b3d;color:#e4e4e7;padding:8px 12px;border-radius:6px;font-size:14px;outline:none}
.toolbar input:focus,.toolbar select:focus{border-color:#7c8aff}
.toolbar button{background:#7c8aff;color:#fff;border:none;padding:8px 18px;border-radius:6px;cursor:pointer;font-size:14px;font-weight:500}
.toolbar button:hover{background:#6a7ae0}
.btn-primary{background:#7c8aff;color:#fff;border:none;padding:8px 18px;border-radius:6px;cursor:pointer;font-size:14px;font-weight:500}
.btn-primary:hover{background:#6a7ae0}
.tabs{display:flex;gap:0;margin-bottom:24px;border-bottom:1px solid #2a2b3d}
.tab{padding:10px 20px;cursor:pointer;font-size:14px;font-weight:500;color:#888;border-bottom:2px solid transparent;transition:all .2s}
.tab:hover{color:#ccc}
.tab.active{color:#7c8aff;border-bottom-color:#7c8aff}
.summary{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:14px;margin-bottom:28px}
.card{background:#1a1b26;border:1px solid #2a2b3d;border-radius:10px;padding:18px}
.card .label{font-size:13px;color:#888;margin-bottom:6px}
.card .value{font-size:1.6rem;font-weight:700;color:#fff}
.card .value.blue{color:#7c8aff}
.card .value.green{color:#34d399}
.card .value.amber{color:#fbbf24}
.card .value.rose{color:#fb7185}
h2{font-size:1.1rem;font-weight:600;margin-bottom:14px;color:#ccc}
table{width:100%;border-collapse:collapse;background:#1a1b26;border-radius:10px;overflow:hidden;border:1px solid #2a2b3d}
th,td{padding:12px 16px;text-align:left;font-size:14px}
th{background:#22243a;color:#999;font-weight:500;border-bottom:1px solid #2a2b3d}
tr:not(:last-child) td{border-bottom:1px solid #1e1f30}
tr:hover td{background:#1e1f30}
.empty{text-align:center;color:#555;padding:40px 0}
.section{margin-bottom:28px}
.badge{display:inline-block;padding:2px 8px;border-radius:4px;font-size:12px;font-weight:500}
.badge.zhipu{background:#1a3a2a;color:#34d399}
.badge.minimax{background:#3a2a1a;color:#fbbf24}
.badge.openai{background:#1a2a3a;color:#60a5fa}
.err{color:#fb7185;font-size:14px;margin-bottom:12px}
.provider-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(280px,1fr));gap:16px;margin-bottom:24px}
.provider-card{background:#1a1b26;border:1px solid #2a2b3d;border-radius:12px;padding:20px;position:relative;overflow:hidden}
.provider-card .card-header{display:flex;align-items:center;gap:10px;margin-bottom:16px}
.provider-card .card-header h3{font-size:1rem;font-weight:600;color:#fff}
.provider-card .card-header .plan-badge{margin-left:auto;font-size:12px;padding:3px 10px;border-radius:20px;font-weight:500}
.plan-badge.plus{background:#1a2a3a;color:#60a5fa}
.plan-badge.pro{background:#1a3a2a;color:#34d399}
.status-dot{width:8px;height:8px;border-radius:50%;flex-shrink:0}
.status-dot.ok{background:#34d399;box-shadow:0 0 6px #34d399}
.status-dot.error{background:#fb7185;box-shadow:0 0 6px #fb7185}
.status-dot.not_configured{background:#555}
.provider-card .error-msg{color:#fb7185;font-size:13px;margin-bottom:12px}
.provider-card .not-configured-msg{color:#555;font-size:13px;text-align:center;padding:20px 0}
.provider-card .metric{margin-bottom:14px}
.provider-card .metric:last-child{margin-bottom:0}
.provider-card .metric-label{font-size:12px;color:#888;margin-bottom:6px;display:flex;justify-content:space-between;align-items:center}
.provider-card .metric-label .meta{color:#666;font-size:11px}
.progress-bar{height:8px;background:#2a2b3d;border-radius:4px;overflow:hidden;position:relative}
.progress-fill{height:100%;border-radius:4px;transition:width .6s ease,background .3s}
.progress-fill.green{background:#34d399}
.progress-fill.yellow{background:#fbbf24}
.progress-fill.red{background:#fb7185}
.provider-card .metric-detail{font-size:12px;color:#666;margin-top:4px}
.skeleton{background:linear-gradient(90deg,#1a1b26 25%,#22243a 50%,#1a1b26 75%);background-size:200% 100%;animation:shimmer 1.5s infinite;border-radius:4px}
@keyframes shimmer{0%{background-position:200% 0}100%{background-position:-200% 0}}
</style>
</head>
<body>
<div class="container">
<h1>🤖 Omni<span>Proxy</span></h1>
<div class="toolbar">
  <input id="keyInput" type="password" placeholder="Admin Key">
  <select id="daysSelect">
    <option value="7">近 7 天</option>
    <option value="30">近 30 天</option>
    <option value="90">近 90 天</option>
    <option value="365">近 1 年</option>
  </select>
  <button onclick="refresh()">刷新</button>
</div>
<div class="tabs">
  <div class="tab active" data-tab="stats" onclick="switchTab('stats')">📊 统计</div>
  <div class="tab" data-tab="keys" onclick="switchTab('keys')">🔑 Key 管理</div>
  <div class="tab" data-tab="providers" onclick="switchTab('providers')">🔌 Provider 状态</div>
</div>
<div id="error" class="err" style="display:none"></div>
<div id="statsTab">
<div id="content" style="display:none">
  <div class="summary" id="summary"></div>
  <div class="section"><h2>🔑 按 Key 统计</h2><table><thead><tr><th>Key</th><th>Provider</th><th>请求数</th><th>Prompt</th><th>Completion</th><th>Total Tokens</th></tr></thead><tbody id="keyBody"></tbody></table></div>
  <div class="section"><h2>📊 按模型统计</h2><table><thead><tr><th>模型</th><th>Provider</th><th>请求数</th><th>Total Tokens</th></tr></thead><tbody id="modelBody"></tbody></table></div>
</div>
<div id="placeholder" class="empty">输入 Admin Key 后点击刷新</div>
</div>
<div id="providersTab" style="display:none">
  <div id="providersPlaceholder" class="empty">输入 Admin Key 后点击刷新</div>
  <div id="providerGrid" class="provider-grid" style="display:none">
    <div class="provider-card" id="card-codex"></div>
    <div class="provider-card" id="card-minimax"></div>
    <div class="provider-card" id="card-zhipu"></div>
  </div>
  <div id="providersError" class="err" style="display:none"></div>
</div>
<div id="keysTab" style="display:none">
  <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:16px">
    <h2 style="margin:0">API Keys</h2>
    <button class="btn-primary" onclick="showKeyModal()">+ 新增 Key</button>
  </div>
  <table><thead><tr><th>别名</th><th>Key</th><th>Provider</th><th>模型限制</th><th>操作</th></tr></thead><tbody id="keysBody"></tbody></table>
</div>
</div>
<script>
var savedKey=localStorage.getItem('gp_admin_key')||'';
document.getElementById('keyInput').value=savedKey;
if(savedKey)refresh();
var usageRefreshTimer=null;
var currentTab='stats';
function switchTab(tab){currentTab=tab;document.querySelectorAll('.tab').forEach(function(t){t.classList.toggle('active',t.dataset.tab===tab)});document.getElementById('statsTab').style.display=tab==='stats'?'block':'none';document.getElementById('providersTab').style.display=tab==='providers'?'block':'none';document.getElementById('keysTab').style.display=tab==='keys'?'block':'none';if(tab==='providers'&&savedKey)loadUsage();if(tab==='keys'&&savedKey)loadKeys();if(tab!=='providers'&&usageRefreshTimer){clearInterval(usageRefreshTimer);usageRefreshTimer=null;}}
function refresh(){var key=document.getElementById('keyInput').value.trim();if(!key){showErr('请输入 Admin Key');return;}savedKey=key;localStorage.setItem('gp_admin_key',key);loadStats();if(currentTab==='providers')loadUsage();}
function loadStats(){fetch('/admin/stats?days='+document.getElementById('daysSelect').value,{headers:{'Authorization':'Bearer '+savedKey}}).then(function(r){if(!r.ok)throw new Error(r.status===401?'Admin Key 错误':'请求失败('+r.status+')');return r.json()}).then(function(d){renderStats(d)}).catch(function(e){showErr(e.message)});}
function loadUsage(){if(usageRefreshTimer){clearInterval(usageRefreshTimer);usageRefreshTimer=null;}fetchUsage();usageRefreshTimer=setInterval(fetchUsage,30000);}
function fetchUsage(){document.getElementById('providersPlaceholder').style.display='none';document.getElementById('providerGrid').style.display='grid';document.getElementById('providersError').style.display='none';renderSkeletons();fetch('/admin/usage',{headers:{'Authorization':'Bearer '+savedKey}}).then(function(r){if(!r.ok)throw new Error(r.status===401?'Admin Key 错误':'请求失败('+r.status+')');return r.json()}).then(function(d){renderProviders(d)}).catch(function(e){document.getElementById('providersError').textContent=e.message;document.getElementById('providersError').style.display='block';});}
function showErr(m){document.getElementById('error').textContent=m;document.getElementById('error').style.display='block';}
function renderStats(d){document.getElementById('error').style.display='none';document.getElementById('content').style.display='block';document.getElementById('placeholder').style.display='none';var s=d.summary||{};document.getElementById('summary').innerHTML='<div class="card"><div class="label">总请求数</div><div class="value blue">'+fmt(s.total_requests)+'</div></div><div class="card"><div class="label">Prompt Tokens</div><div class="value green">'+fmt(s.total_prompt_tokens)+'</div></div><div class="card"><div class="label">Completion Tokens</div><div class="value amber">'+fmt(s.total_completion_tokens)+'</div></div><div class="card"><div class="label">Total Tokens</div><div class="value rose">'+fmt(s.total_tokens)+'</div></div>';var kb=document.getElementById('keyBody');if(d.by_key&&d.by_key.length){kb.innerHTML=d.by_key.map(function(k){return '<tr><td>'+esc(k.key_alias)+'</td><td><span class="badge '+(k.provider||'')+'">'+esc(k.provider)+'</span></td><td>'+fmt(k.requests)+'</td><td>'+fmt(k.prompt_tokens)+'</td><td>'+fmt(k.completion_tokens)+'</td><td>'+fmt(k.total_tokens)+'</td></tr>'}).join('')}else{kb.innerHTML='<tr><td colspan="6" class="empty">暂无数据</td></tr>'}var mb=document.getElementById('modelBody');if(d.by_model&&d.by_model.length){mb.innerHTML=d.by_model.map(function(m){return '<tr><td>'+esc(m.model)+'</td><td><span class="badge '+(m.provider||'')+'">'+esc(m.provider)+'</span></td><td>'+fmt(m.requests)+'</td><td>'+fmt(m.total_tokens)+'</td></tr>'}).join('')}else{mb.innerHTML='<tr><td colspan="4" class="empty">暂无数据</td></tr>'}}
function renderSkeletons(){['codex','minimax','zhipu'].forEach(function(n){document.getElementById('card-'+n).innerHTML='<div class="card-header"><div class="skeleton" style="width:80px;height:16px"></div></div><div class="metric"><div class="skeleton" style="width:100%;height:8px;margin-bottom:8px"></div><div class="skeleton" style="width:60%;height:8px"></div></div><div class="metric"><div class="skeleton" style="width:100%;height:8px;margin-bottom:8px"></div><div class="skeleton" style="width:60%;height:8px"></div></div>';})}
function renderProviders(d){var p=d.providers||{};renderCodexCard(p.codex);renderMiniMaxCard(p.minimax);renderZhipuCard(p.zhipu);}
function renderCodexCard(c){var el=document.getElementById('card-codex');if(!c){el.innerHTML='<div class="card-header"><div class="status-dot not_configured"></div><h3>Codex (ChatGPT)</h3></div><div class="not-configured-msg">未配置</div>';return;}if(c.status==='not_configured'){el.innerHTML='<div class="card-header"><div class="status-dot not_configured"></div><h3>Codex (ChatGPT)</h3></div><div class="not-configured-msg">未配置</div>';return;}if(c.status==='error'){el.innerHTML='<div class="card-header"><div class="status-dot error"></div><h3>Codex (ChatGPT)</h3></div><div class="error-msg">'+esc(c.error||'查询失败')+'</div>';return;}var planBadge=c.plan?'<span class="plan-badge '+(c.plan==='pro'?'pro':'plus')+'">'+esc(c.plan.toUpperCase())+'</span>':'';var w5h=c['5h_window']||{};var ww=c.weekly_window||{};el.innerHTML='<div class="card-header"><div class="status-dot ok"></div><h3>Codex (ChatGPT)</h3>'+planBadge+'</div>'+buildMetric('5 小时窗口','已用 '+w5h.used_percent+'%',w5h.used_percent,w5h.reset_seconds)+buildMetric('周窗口','已用 '+ww.used_percent+'%',ww.used_percent,ww.reset_seconds);}
function renderMiniMaxCard(c){var el=document.getElementById('card-minimax');if(!c){el.innerHTML='<div class="card-header"><div class="status-dot not_configured"></div><h3>MiniMax</h3></div><div class="not-configured-msg">未配置</div>';return;}if(c.status==='not_configured'){el.innerHTML='<div class="card-header"><div class="status-dot not_configured"></div><h3>MiniMax</h3></div><div class="not-configured-msg">未配置</div>';return;}if(c.status==='error'){el.innerHTML='<div class="card-header"><div class="status-dot error"></div><h3>MiniMax</h3></div><div class="error-msg">'+esc(c.error||'查询失败')+'</div>';return;}var w=c['5h_window']||{};var pct=w.used_percent||0;el.innerHTML='<div class="card-header"><div class="status-dot ok"></div><h3>MiniMax</h3>'+(c.model?'<span class="plan-badge plus">'+esc(c.model)+'</span>':'')+'</div>'+buildMetric('5 小时窗口',fmt(c.used||0)+' / '+fmt(c.total||0),pct,w.reset_seconds);}
function renderZhipuCard(c){var el=document.getElementById('card-zhipu');if(!c){el.innerHTML='<div class="card-header"><div class="status-dot not_configured"></div><h3>智谱 GLM</h3></div><div class="not-configured-msg">未配置</div>';return;}if(c.status==='not_configured'){el.innerHTML='<div class="card-header"><div class="status-dot not_configured"></div><h3>智谱 GLM</h3></div><div class="not-configured-msg">未配置</div>';return;}if(c.status==='error'){el.innerHTML='<div class="card-header"><div class="status-dot error"></div><h3>智谱 GLM</h3></div><div class="error-msg">'+esc(c.error||'查询失败')+'</div>';return;}var planBadge=c.plan?'<span class="plan-badge '+(c.plan==='pro'?'pro':'plus')+'">'+esc(c.plan.toUpperCase())+'</span>':'';var mcp=c['mcp_monthly']||{};var tok=c['tokens_5h']||{};var html='<div class="card-header"><div class="status-dot ok"></div><h3>智谱 GLM</h3>'+planBadge+'</div>'+buildMetric('MCP 工具 (月)',fmt(mcp.used||0)+' / '+fmt(mcp.total||0)+' (剩余 '+fmt(mcp.remaining||0)+')',mcp.percentage||0,mcp.reset_seconds);if(tok.percentage!==undefined){html+=buildMetric('模型 Tokens (5h)','已用 '+tok.percentage+'%',tok.percentage||0,tok.reset_seconds);}el.innerHTML=html;}
function buildMetric(label,detailText,pct,resetSec){var color=pct<50?'green':pct<80?'yellow':'red';var resetHtml=resetSec!=null?'<span class="meta">'+fmtTime(resetSec)+'后重置</span>':'';return '<div class="metric"><div class="metric-label"><span>'+esc(label)+'</span>'+resetHtml+'</div><div class="progress-bar"><div class="progress-fill '+color+'" style="width:'+Math.min(pct,100)+'%"></div></div><div class="metric-detail">'+detailText+'</div></div>';}
function fmtTime(sec){if(sec==null||sec<=0)return '0秒';var d=Math.floor(sec/86400);var h=Math.floor((sec%86400)/3600);var m=Math.floor((sec%3600)/60);if(d>0&&h>0)return d+'天'+h+'小时';if(d>0)return d+'天';if(h>0&&m>0)return h+'小时'+m+'分钟';if(h>0)return h+'小时';return m+'分钟';}
function fmt(n){return(n||0).toLocaleString()}
function esc(s){var d=document.createElement('div');d.textContent=s||'';return d.innerHTML}
// Key Management
function loadKeys(){fetch('/admin/keys',{headers:{'Authorization':'Bearer '+savedKey}}).then(function(r){if(!r.ok)throw new Error(r.status===401?'Admin Key 错误':'请求失败');return r.json()}).then(function(d){renderKeys(d.keys||[])}).catch(function(e){showErr(e.message)});}
function renderKeys(keys){var b=document.getElementById('keysBody');if(!keys.length){b.innerHTML='<tr><td colspan="5" class="empty">暂无 Key</td></tr>';return;}b.innerHTML=keys.map(function(k){var models=k.models&&k.models.length?k.models.join(', '):'不限制';var adminBadge=k.is_admin?'<span style="color:#fbbf24;font-size:11px;margin-left:6px">⭐ Admin</span>':'';var actions=k.is_admin?'':('<button onclick="editKey(\''+esc(k.alias)+'\')" style="background:#2a2b3d;color:#e4e4e7;border:none;padding:4px 12px;border-radius:4px;cursor:pointer;font-size:13px;margin-right:6px">编辑</button>'+'<button onclick="deleteKey(\''+esc(k.alias)+'\')" style="background:#3a1a1a;color:#fb7185;border:none;padding:4px 12px;border-radius:4px;cursor:pointer;font-size:13px">删除</button>');return '<tr><td>'+esc(k.alias)+adminBadge+'</td><td style="font-family:monospace;font-size:13px;color:#888">'+esc(k.key_masked)+'</td><td>'+( k.provider||'不限制')+'</td><td style="font-size:13px">'+models+'</td><td>'+actions+'</td></tr>';}).join('');}
function showKeyModal(existing){var m=document.getElementById('keyModal');if(!m){m=document.createElement('div');m.id='keyModal';m.style.cssText='position:fixed;top:0;left:0;right:0;bottom:0;background:rgba(0,0,0,0.6);display:flex;align-items:center;justify-content:center;z-index:999';document.body.appendChild(m);}var isEdit=!!existing;m.innerHTML='<div style="background:#1a1b26;border:1px solid #2a2b3d;border-radius:12px;padding:28px;width:420px;max-width:90vw">'+('<h2 style="margin:0 0 20px;font-size:1.1rem;color:#e4e4e7">'+(isEdit?'编辑 Key':'新增 Key')+'</h2>')+'<div style="margin-bottom:14px"><label style="display:block;font-size:13px;color:#888;margin-bottom:6px">别名</label><input id="mAlias" value="'+(existing?esc(existing.alias):'')+'" style="width:100%;background:#0f1117;border:1px solid #2a2b3d;color:#e4e4e7;padding:8px 12px;border-radius:6px;font-size:14px;outline:none"></div>'+'<div style="margin-bottom:14px"><label style="display:block;font-size:13px;color:#888;margin-bottom:6px">Provider 限制</label><input id="mProvider" value="'+(existing?esc(existing.provider||''):'' )+'" placeholder="留空 = 不限制" style="width:100%;background:#0f1117;border:1px solid #2a2b3d;color:#e4e4e7;padding:8px 12px;border-radius:6px;font-size:14px;outline:none"></div>'+'<div style="margin-bottom:20px"><label style="display:block;font-size:13px;color:#888;margin-bottom:6px">模型限制（逗号分隔，留空 = 不限制）</label><input id="mModels" value="'+(existing&&existing.models&&existing.models.length?esc(existing.models.join(', ')):'')+'" placeholder="例如: gpt-5.5, o3" style="width:100%;background:#0f1117;border:1px solid #2a2b3d;color:#e4e4e7;padding:8px 12px;border-radius:6px;font-size:14px;outline:none"></div>'+'<div style="display:flex;gap:10px;justify-content:flex-end"><button onclick="closeKeyModal()" style="background:#2a2b3d;color:#e4e4e7;border:none;padding:8px 20px;border-radius:6px;cursor:pointer">取消</button><button onclick="saveKey('+(isEdit?'\''+esc(existing.alias)+'\'':'null')+')" style="background:#7c8aff;color:#fff;border:none;padding:8px 20px;border-radius:6px;cursor:pointer">保存</button></div>'+'</div>';}
function closeKeyModal(){var m=document.getElementById('keyModal');if(m)m.remove();}
function saveKey(editAlias){var alias=document.getElementById('mAlias').value.trim();var prov=document.getElementById('mProvider').value.trim();var modelsStr=document.getElementById('mModels').value.trim();var models=[];if(modelsStr){models=modelsStr.split(',').map(function(s){return s.trim()}).filter(function(s){return s});}if(!alias){alert('别名不能为空');return;}if(editAlias){fetch('/admin/keys/'+encodeURIComponent(editAlias),{method:'PUT',headers:{'Authorization':'Bearer '+savedKey,'Content-Type':'application/json'},body:JSON.stringify({alias:alias,provider:prov,models:models})}).then(function(r){if(!r.ok)return r.json().then(function(d){throw new Error(d.error||'修改失败')});closeKeyModal();loadKeys();}).catch(function(e){alert(e.message);});}else{fetch('/admin/keys',{method:'POST',headers:{'Authorization':'Bearer '+savedKey,'Content-Type':'application/json'},body:JSON.stringify({alias:alias,provider:prov,models:models})}).then(function(r){if(!r.ok)return r.json().then(function(d){throw new Error(d.error||'新增失败')});return r.json();}).then(function(d){closeKeyModal();loadKeys();alert('Key 已创建！请保存完整 Key（仅显示一次）：\n'+d.key);}).catch(function(e){alert(e.message);});}}
function editKey(alias){fetch('/admin/keys',{headers:{'Authorization':'Bearer '+savedKey}}).then(function(r){return r.json()}).then(function(d){var k=(d.keys||[]).find(function(x){return x.alias===alias});if(k)showKeyModal(k);}).catch(function(e){alert(e.message);});}
function deleteKey(alias){if(!confirm('确定删除 Key "'+alias+'"？此操作不可恢复。'))return;fetch('/admin/keys/'+encodeURIComponent(alias),{method:'DELETE',headers:{'Authorization':'Bearer '+savedKey}}).then(function(r){if(!r.ok)return r.json().then(function(d){throw new Error(d.error||'删除失败')});loadKeys();}).catch(function(e){alert(e.message);});}
</script>
</body>
</html>`
