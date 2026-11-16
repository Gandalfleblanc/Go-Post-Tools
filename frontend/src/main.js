import './style.css'
import App from './App.svelte'
import { initZoom } from './zoom.js'

initZoom();

const app = new App({
  target: document.getElementById('app')
})

export default app