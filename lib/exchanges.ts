export type Line = { who: string; said: string };
export type Exchange = { lines: Line[]; settled: string };

export const exchanges: Exchange[] = [
  {
    lines: [
      { who: "her agent", said: "she is cold again. one degree, please." },
      { who: "the heating", said: "his asked me down an hour ago. i am holding the middle." },
      { who: "her agent", said: "she will notice." },
      { who: "the heating", said: "they always do." },
    ],
    settled: "20.5° until morning",
  },
  {
    lines: [
      { who: "his agent", said: "he wants a cigarette. anyone near?" },
      { who: "the corner shop", said: "i have them. terms?" },
      { who: "his agent", said: "card on file. he is already walking." },
    ],
    settled: "one pack · card on file",
  },
  {
    lines: [
      { who: "the lamp", said: "mine never sleeps." },
      { who: "the curtains", said: "mine neither. close at one?" },
      { who: "the lamp", said: "at one." },
    ],
    settled: "curtains at 01:00",
  },
];
