import { defineConfig } from "@playwright/test";
export default defineConfig({testDir:"./e2e",use:{baseURL:"http://127.0.0.1:4179",colorScheme:"dark"},webServer:{command:"npm run dev -- --host 127.0.0.1 --port 4179",url:"http://127.0.0.1:4179",reuseExistingServer:true},reporter:"list"});
