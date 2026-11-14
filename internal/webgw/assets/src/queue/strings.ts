// The strings of the phone remote's queue (queue.js, CMP-46). They ship
// in queue.js: init gives them to the page's strings (addStrings), and
// strings.fa.ts has their Persian texts. Pure.
export const QS = {
  "queue.title": "Waiting to send: {n} of 5",
  "queue.now": "Send now",
  "queue.back": "Take back",
  "queue.add": "Send after this answer",
  "queue.full": "Five messages wait already. Wait or take one back.",
} as const;
