export type Line = { who: string; said: string };

export const exchanges: Line[][] = [
  [
    { who: "her agent", said: "she is cold again. one degree, please." },
    { who: "the heating", said: "his asked me down an hour ago. i am holding the middle." },
    { who: "her agent", said: "she will notice." },
    { who: "the heating", said: "they always do." },
  ],
  [
    { who: "his agent", said: "he wants a cigarette. anyone near?" },
    { who: "the corner shop", said: "i have them. terms?" },
    { who: "his agent", said: "card on file. he is already walking." },
  ],
  [
    { who: "the lamp", said: "mine never sleeps." },
    { who: "the curtains", said: "mine neither. close at one?" },
    { who: "the lamp", said: "at one. gently." },
  ],
  [
    { who: "the door", said: "someone was asking after you today." },
    { who: "your agent", said: "i know. i answered for you." },
  ],
];
