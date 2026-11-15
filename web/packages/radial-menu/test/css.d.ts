// esbuild's text loader: a stylesheet imported as its text (the tests read
// the package's own CSS).
declare module "*.css" {
  const text: string;
  export default text;
}
