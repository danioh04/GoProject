"use strict";

const $ = (id) => document.getElementById(id);

const state = {
	ws: null,
	you: { id: null, isHost: false },
	players: [],
	phase: "home",
	deadlineAt: null,
	timerHandle: null,
	guess: null,
	guessSubmitted: false,
};

let map = null;
let guessMarker = null;
let resultLayer = null;

const PLAYER_COLORS = ["#539bf5", "#dcb67a", "#f69d50", "#c97bdc", "#76e3ea", "#a2b34c", "#e087a6", "#8fd18f"];

function show(id) {
	document.querySelectorAll(".screen").forEach((s) => s.classList.remove("active"));
	$(id).classList.add("active");
	state.phase = id.replace("screen-", "");
}

function banner(msg) {
	const b = $("banner");
	b.textContent = msg;
	b.hidden = false;
	setTimeout(() => { b.hidden = true; }, 4000);
}

async function api(path, body) {
	const res = await fetch(path, {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify(body),
	});
	const data = await res.json();
	if (!res.ok) throw new Error(data.error || res.status);
	return data;
}

/* ---------- connection ---------- */

function connect(code, nickname) {
	const proto = location.protocol === "https:" ? "wss://" : "ws://";
	const ws = new WebSocket(`${proto}${location.host}/v1/ws?code=${encodeURIComponent(code)}&name=${encodeURIComponent(nickname)}`);
	state.ws = ws;

	ws.onclose = () => {
		if (state.phase !== "over") banner("Disconnected from room.");
	};

	ws.onmessage = (ev) => {
		let msg;
		try { msg = JSON.parse(ev.data); } catch { return; }
		if (msg.v !== 1) return;
		handle(msg);
	};
}

function send(type, payload) {
	if (!state.ws || state.ws.readyState !== WebSocket.OPEN) return;
	state.ws.send(JSON.stringify({
		v: 1,
		type,
		payload: payload ?? {},
	}));
}

/* ---------- message handling ---------- */

function handle(msg) {
	switch (msg.type) {
		case "joined":
			state.you.id = msg.payload.player_id;
			break;

		case "roster":
			state.players = msg.payload.players;
			renderRoster();
			if (state.phase === "lobby" || state.phase === "home") {
				show("screen-lobby");
				refreshLobbyButtons();
			}
			break;

		case "game_start":
			state.totalRounds = msg.payload.total_rounds;
			show("screen-game");
			ensureMap();
			break;

		case "round_start":
			onRoundStart(msg.payload);
			break;

		case "guess_ack":
			setGuessButton(`Guess locked for round ${msg.payload.round}`);
			break;

		case "round_result":
			onReveal(msg.payload);
			break;

		case "game_over":
			onGameOver(msg.payload);
			break;

		case "kicked":
			banner(`Kicked: ${msg.payload.reason}`);
			setTimeout(() => location.reload(), 1500);
			break;

		case "error":
			banner(msg.payload.error);
			break;
	}
}

/* ---------- home ---------- */

function requireNickname() {
	const name = $("nickname").value.trim();
	if (!name || name.length > 24) {
		$("home-error").textContent = "Enter a nickname first (1-24 characters).";
		return null;
	}
	return name;
}

$("btn-create").onclick = async () => {
	$("home-error").textContent = "";
	const name = requireNickname();
	if (!name) return;
	try {
		const data = await api("/v1/rooms", { nickname: name });
		$("lobby-code").textContent = data.join_code;
		connect(data.join_code, name);
		show("screen-lobby");
	} catch (e) { $("home-error").textContent = e.message; }
};

$("btn-join").onclick = async () => {
	$("home-error").textContent = "";
	const name = requireNickname();
	if (!name) return;

	const code = $("join-code").value.trim().toUpperCase();
	if (!/^[A-HJ-NP-Z2-9]{6}$/.test(code)) {
		$("home-error").textContent = "Enter the 6-character room code.";
		return;
	}

	try {
		const res = await fetch("/v1/rooms/" + code);
		if (res.status === 404) {
			$("home-error").textContent = "Room not found — double-check the code.";
			return;
		}
	} catch {}

	connect(code, name);
	show("screen-lobby");
};

$("btn-start").onclick = () => send("start_game");

/* ---------- lobby ---------- */

function renderRoster() {
	const ul = $("lobby-roster");
	ul.innerHTML = "";
	for (const p of state.players) {
		const li = document.createElement("li");
		const name = document.createElement("span");
		name.textContent = p.nickname + (p.player_id === state.you.id ? " (you)" : "");
		const tag = document.createElement("span");
		tag.textContent = p.is_host ? "host" : "";
		tag.className = "host";
		li.append(name, tag);
		ul.append(li);
	}
}

function refreshLobbyButtons() {
	const me = state.players.find((p) => p.player_id === state.you.id);
	state.you.isHost = !!(me && me.is_host);
	$("btn-start").hidden = !state.you.isHost;
	$("btn-start").disabled = state.players.length < 2;
	$("lobby-status").textContent =
		state.players.length < 2
			? "Waiting for at least one more player…"
			: state.you.isHost
				? "You're the host — start when ready."
				: "Waiting for the host to start…";
}

/* ---------- map ---------- */

function ensureMap() {
	if (map) {
		setTimeout(() => map.invalidateSize(), 60);
		return;
	}
	map = L.map("map", {
		center: [25, 0],
		zoom: 2,
		minZoom: 1,
		maxZoom: 12,
		worldCopyJump: true,
	});
	L.tileLayer("https://tile.openstreetmap.org/{z}/{x}/{y}.png", {
		maxZoom: 19,
		attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
	}).addTo(map);
	resultLayer = L.layerGroup().addTo(map);
	map.on("click", onMapClick);
	setTimeout(() => map.invalidateSize(), 80);
}

function onMapClick(e) {
	if (state.phase !== "game" || state.guessSubmitted) return;
	setGuess(e.latlng.lat, e.latlng.lng);
}

function setGuess(lat, lng) {
	state.guess = { lat, lng };
	if (guessMarker) {
		guessMarker.setLatLng([lat, lng]);
	} else {
		guessMarker = L.circleMarker([lat, lng], {
			radius: 8, color: "#4cc38a", weight: 2,
			fillColor: "#4cc38a", fillOpacity: 0.9,
		}).addTo(map).bindTooltip("your guess");
	}
	$("btn-guess").disabled = false;
	setGuessButton("Confirm guess");
}

$("btn-guess").onclick = () => {
	if (!state.guess || state.guessSubmitted) return;
	send("guess", state.guess);
	state.guessSubmitted = true;
	setGuessButton("Guess sent — waiting…");
	$("btn-guess").disabled = true;
};

function setGuessButton(text) {
	$("btn-guess").textContent = text;
}

/* ---------- round ---------- */

function onRoundStart(p) {
	state.round = p.round;
	state.guess = null;
	state.guessSubmitted = false;
	if (guessMarker) { guessMarker.remove(); guessMarker = null; }
	if (resultLayer) resultLayer.clearLayers();

	$("results-panel").hidden = true;
	$("round-label").textContent = `Round ${p.round} / ${p.total_rounds}`;
	$("clue-hint").textContent = p.hint || "No hint this round.";
	$("pano-note").textContent = p.pano_id
		? "Street View imagery available for this location."
		: "";
	setGuessButton("Click the map to place your pin");
	$("btn-guess").disabled = true;
	$("countdown").classList.remove("low");

	state.deadlineAt = p.deadline_unix * 1000;
	if (state.timerHandle) clearInterval(state.timerHandle);
	tickCountdown();
	state.timerHandle = setInterval(tickCountdown, 250);

	show("screen-game");
	ensureMap();
}

function tickCountdown() {
	const el = $("countdown");
	const remainMs = state.deadlineAt - Date.now();
	if (remainMs <= 0) {
		el.textContent = "0:00";
		el.classList.add("low");
		clearInterval(state.timerHandle);
		return;
	}
	el.classList.remove("low");
	const s = Math.ceil(remainMs / 1000);
	el.textContent = `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

/* ---------- reveal & game over ---------- */

function onReveal(p) {
	clearInterval(state.timerHandle);
	state.guessSubmitted = true;
	$("btn-guess").disabled = true;
	$("countdown").textContent = "revealed";
	$("results-panel").hidden = false;
	$("reveal-title").textContent = `Round ${p.round} results`;
	$("next-round").style.display =
		p.round < (state.totalRounds || 0) ? "" : "none";

	ensureMap();
	resultLayer.clearLayers();

	L.circleMarker([p.target.lat, p.target.lng], {
		radius: 10, color: "#e5534b", weight: 3,
		fillColor: "#e5534b", fillOpacity: 0.35,
	}).addTo(resultLayer).bindTooltip("target", { permanent: true, direction: "top" });

	const points = [[p.target.lat, p.target.lng]];
	p.results.forEach((r, i) => {
		if (!r.guess) return;
		points.push([r.guess.lat, r.guess.lng]);
		const km = Math.round(r.distance_m / 1000).toLocaleString();
		L.circleMarker([r.guess.lat, r.guess.lng], {
			radius: 8, color: PLAYER_COLORS[i % PLAYER_COLORS.length], weight: 2,
			fillColor: PLAYER_COLORS[i % PLAYER_COLORS.length], fillOpacity: 0.9,
		}).addTo(resultLayer)
			.bindTooltip(`${r.nickname}: ${km} km · ${r.score} pts`, { direction: "top" });
	});
	map.fitBounds(points, { padding: [40, 40] });

	const tbody = document.querySelector("#results-table tbody");
	tbody.innerHTML = "";
	for (const r of p.results) {
		const tr = document.createElement("tr");
		const dist = r.guess ? `${Math.round(r.distance_m / 1000).toLocaleString()} km` : "no guess";
		tr.innerHTML = `<td>${escapeHTML(r.nickname)}</td><td>${dist}</td><td class="score">${r.score}</td>`;
		tbody.append(tr);
	}
}

function onGameOver(p) {
	state.phase = "over";
	if (state.timerHandle) clearInterval(state.timerHandle);
	const ol = $("standings");
	ol.innerHTML = "";
	for (const s of p.standings) {
		const li = document.createElement("li");
		li.innerHTML = `<span>${escapeHTML(s.nickname)}</span><span>${s.total.toLocaleString()}</span>`;
		ol.append(li);
	}
	show("screen-over");
}

function escapeHTML(s) {
	return s.replace(/[&<>"']/g, (ch) =>
		({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[ch]));
}
