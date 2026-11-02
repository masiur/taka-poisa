import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [
		sveltekit({
			// Built straight into the Go embed directory; the Go server falls back to index.html.
			adapter: adapter({ pages: '../internal/webui/dist', fallback: 'index.html' })
		})
	],
	server: {
		proxy: { '/api': 'http://localhost:8080' }
	}
});
