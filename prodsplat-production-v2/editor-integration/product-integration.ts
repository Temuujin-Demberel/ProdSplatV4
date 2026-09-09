import { MemoryFileSystem } from '@playcanvas/splat-transform';

import { Events } from './events';
import { Splat } from './splat';
import { writeSplatFile, type SerializeSettings } from './splat-serialize';

class CaptureFileSystem extends MemoryFileSystem {
    firstResult(): Uint8Array {
        for (const [, data] of this.results.entries()) {
            return data;
        }
        throw new Error('SuperSplat serialization produced no output');
    }
}

const styleButton = (button: HTMLButtonElement, primary = false) => {
    Object.assign(button.style, {
        padding: '9px 12px',
        borderRadius: '8px',
        border: primary ? '1px solid #fff' : '1px solid #505866',
        background: primary ? '#f3f5f7' : '#252b33',
        color: primary ? '#11151a' : '#f3f5f7',
        font: '600 12px system-ui',
        cursor: 'pointer'
    });
};

const registerProductIntegration = (events: Events) => {
    const params = new URLSearchParams(window.location.search);
    const jobId = params.get('job');
    if (!jobId) return;

    const panel = document.createElement('div');
    Object.assign(panel.style, {
        position: 'fixed',
        top: '16px',
        right: '16px',
        zIndex: '100000',
        display: 'grid',
        gap: '8px',
        minWidth: '280px',
        padding: '12px',
        borderRadius: '12px',
        background: 'rgba(15, 18, 23, .94)',
        boxShadow: '0 16px 50px rgba(0,0,0,.45)',
        border: '1px solid rgba(255,255,255,.12)',
        backdropFilter: 'blur(12px)'
    });

    const title = document.createElement('div');
    title.innerHTML = `<strong style="font:600 13px system-ui;color:#fff">ProdSplat review</strong><br><span style="font:11px monospace;color:#9eb0c2">${jobId}</span>`;

    const hint = document.createElement('div');
    hint.textContent = 'Clean the product with SuperSplat selection/delete/transform tools, then save the edited Gaussian asset.';
    Object.assign(hint.style, {font:'11px/1.4 system-ui', color:'#b8c0ca'});

    const status = document.createElement('div');
    Object.assign(status.style, {font:'11px system-ui', color:'#9ad7a5', minHeight:'16px'});

    const save = document.createElement('button');
    save.textContent = 'Save & Continue';
    styleButton(save, true);

    const render = document.createElement('button');
    render.textContent = 'Save + Render 32 RGBA Views';
    styleButton(render);

    const dashboard = document.createElement('button');
    dashboard.textContent = 'Back to Dashboard';
    styleButton(dashboard);
    dashboard.onclick = () => window.open('/app/', '_blank');

    const serializeAndUpload = async () => {
        const splats = await events.invoke('scene.splats') as Splat[];
        if (!splats || splats.length === 0) {
            throw new Error('There is no visible Gaussian scene to save.');
        }
        const settings: SerializeSettings = {
            maxSHBands: 3,
            selected: false,
            minOpacity: 0,
            removeInvalid: true,
            keepWorldTransform: false,
            keepColorTint: false
        };
        const fs = new CaptureFileSystem();
        await writeSplatFile(splats, settings, 'ply', 'cleaned.ply', {}, fs);
        const bytes = fs.firstResult();
        const payload = new Blob([bytes as BlobPart], {type: 'application/octet-stream'});
        const response = await fetch(`/api/jobs/${encodeURIComponent(jobId)}/cleaned`, {
            method: 'POST',
            headers: {'Content-Type': 'application/octet-stream'},
            body: payload
        });
        if (!response.ok) throw new Error(await response.text());
        return response.json();
    };

    const run = async (startRender: boolean) => {
        save.disabled = true;
        render.disabled = true;
        status.textContent = 'Serializing edited Gaussian scene…';
        try {
            await serializeAndUpload();
            status.textContent = 'Edited PLY saved ✓';
            if (startRender) {
                status.textContent = 'Starting transparent rendering…';
                const response = await fetch(`/api/jobs/${encodeURIComponent(jobId)}/render`, {method:'POST'});
                if (!response.ok) throw new Error(await response.text());
                status.textContent = 'Render queued ✓';
            }
        } catch (error) {
            console.error(error);
            status.style.color = '#ff9c9c';
            status.textContent = error instanceof Error ? error.message : String(error);
        } finally {
            save.disabled = false;
            render.disabled = false;
        }
    };

    save.onclick = () => void run(false);
    render.onclick = () => void run(true);
    panel.append(title, hint, status, save, render, dashboard);
    document.body.appendChild(panel);
};

export { registerProductIntegration };
