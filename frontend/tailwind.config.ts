import type { Config } from 'tailwindcss';
const config: Config = { content: ['./src/**/*.{ts,tsx}'], theme: { extend: { colors: { canvas:'#1B2428',surface:'#243036',border:'#324149',teal:'#1A7D82',coral:'#F46A6A',muted:'#9FB1B9',success:'#10B981',warning:'#F59E0B' } } }, plugins: [] };
export default config;
