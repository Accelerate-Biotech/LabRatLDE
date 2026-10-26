export type DiffLine = { op: ' ' | '+' | '-'; line: string };

// Line diff from a standard longest-common-subsequence table.
export function lineDiff(a: string, b: string): DiffLine[] {
  const x = a.replace(/\r\n/g, '\n').split('\n');
  const y = b.replace(/\r\n/g, '\n').split('\n');
  // t[i][j] = LCS length of x[i..] and y[j..]
  const t = Array.from({ length: x.length + 1 }, () => new Array<number>(y.length + 1).fill(0));
  for (let i = x.length - 1; i >= 0; i--) {
    for (let j = y.length - 1; j >= 0; j--) {
      t[i][j] = x[i] === y[j] ? t[i + 1][j + 1] + 1 : Math.max(t[i + 1][j], t[i][j + 1]);
    }
  }
  const out: DiffLine[] = [];
  let i = 0;
  let j = 0;
  while (i < x.length || j < y.length) {
    if (i < x.length && j < y.length && x[i] === y[j]) {
      out.push({ op: ' ', line: x[i] });
      i++;
      j++;
    } else if (i < x.length && (j === y.length || t[i + 1][j] >= t[i][j + 1])) {
      out.push({ op: '-', line: x[i++] });
    } else {
      out.push({ op: '+', line: y[j++] });
    }
  }
  return out;
}
