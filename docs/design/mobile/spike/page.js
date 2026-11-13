// The spike page's buttons (index.html); the checks are in spike.js.
(function () {
  "use strict";
  var S = window.tcSpike;
  var rows = [];
  var extra = {};
  var out = document.getElementById("spike-out");
  var md = document.getElementById("md");
  var device = document.getElementById("device");
  try { device.value = localStorage.getItem("tc-spike-device") || ""; } catch (e) { /* none */ }

  function draw() {
    var all = rows.concat(Object.keys(extra).map(function (k) { return extra[k]; }));
    out.textContent = "";
    all.forEach(function (r) {
      var li = document.createElement("li");
      var m = document.createElement("span");
      m.className = "m " + S.mark(r.ok);
      m.textContent = S.mark(r.ok);
      var w = document.createElement("span");
      w.textContent = r.what;
      var d = document.createElement("span");
      d.className = "d";
      d.textContent = r.detail;
      li.append(m, w, d);
      out.appendChild(li);
    });
    md.value = S.markdown(all, device.value.trim());
  }
  function run() { S.all().then(function (r) { rows = r; draw(); }); }
  function one(fn) { return function () { fn().then(function (r) { extra[r.id] = r; draw(); }); }; }

  document.getElementById("make").onclick = one(S.passkeyMake);
  document.getElementById("use").onclick = one(S.passkeyUse);
  document.getElementById("again").onclick = run;
  document.getElementById("copy").onclick = function () {
    md.select();
    if (navigator.clipboard) navigator.clipboard.writeText(md.value).catch(function () { /* the text stays selected */ });
  };
  device.oninput = function () {
    try { localStorage.setItem("tc-spike-device", device.value); } catch (e) { /* none */ }
    draw();
  };
  run();
})();
