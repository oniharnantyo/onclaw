import React, { useState, useEffect } from 'react';
import { ToggleLeft, ToggleRight, Gear, X, Wrench } from '@phosphor-icons/react';
import Tooltip from './Tooltip';

export interface Tool {
	name: string;
	category: string;
	enabled: boolean;
	description?: string;
}

export interface ToolCategory {
	category: string;
	configurable: boolean;
	schema?: string;
	tools: Tool[];
}

interface ToolsProps {
	showToast: (msg: string, type?: 'success' | 'error') => void;
}

export default function Tools({ showToast }: ToolsProps) {
	const [categories, setCategories] = useState<ToolCategory[]>([]);
	const [loading, setLoading] = useState(true);
	
	// Configuration Modal States
	const [showConfigModal, setShowConfigModal] = useState(false);
	const [activeCategory, setActiveCategory] = useState<string | null>(null);
	const [configSchema, setConfigSchema] = useState<string>('');
	const [configJSON, setConfigJSON] = useState<string>('{}');
	const [jsonError, setJsonError] = useState<string | null>(null);
	const [saving, setSaving] = useState(false);

	// Browser Config States
	const [browserEngine, setBrowserEngine] = useState<'lightpanda' | 'chromium' | 'remote'>('lightpanda');
	const [browserHeadless, setBrowserHeadless] = useState<boolean>(true);
	const [lightpandaBinPath, setLightpandaBinPath] = useState<string>('');
	const [lightpandaPort, setLightpandaPort] = useState<number>(9222);
	const [chromiumBinPath, setChromiumBinPath] = useState<string>('');
	const [remoteURL, setRemoteURL] = useState<string>('');

	// Web Config States
	const [searchProvider, setSearchProvider] = useState<string>('duckduckgo');
	const [fetchProvider, setFetchProvider] = useState<string>('http');
	const [userAgent, setUserAgent] = useState<string>('');
	const [timeoutSeconds, setTimeoutSeconds] = useState<number>(10);
	const [maxBytes, setMaxBytes] = useState<number>(1048576);
	const [googleCX, setGoogleCX] = useState<string>('');
	const [webLightpandaBinPath, setWebLightpandaBinPath] = useState<string>('');

	useEffect(() => {
		loadTools();
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, []);

	const loadTools = async () => {
		setLoading(true);
		try {
			const res = await fetch('/api/tools');
			if (res.ok) {
				setCategories(await res.json());
			} else {
				showToast('Failed to load tools registry', 'error');
			}
		} catch {
			showToast('Failed to load tools registry', 'error');
		} finally {
			setLoading(false);
		}
	};

	const toggleTool = async (tool: Tool) => {
		try {
			const res = await fetch(`/api/tools/${encodeURIComponent(tool.name)}/toggle`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ enabled: !tool.enabled }),
			});
			if (res.ok) {
				showToast(
					`Tool "${tool.name}" ${!tool.enabled ? 'enabled' : 'disabled'} successfully`,
					'success'
				);
				loadTools();
			} else {
				const err = await res.json();
				showToast(err.error || 'Failed to toggle tool', 'error');
			}
		} catch {
			showToast('Failed to toggle tool', 'error');
		}
	};

	const openConfig = async (categoryView: ToolCategory) => {
		setActiveCategory(categoryView.category);
		setConfigSchema(categoryView.schema || '');
		setJsonError(null);
		
		try {
			const res = await fetch(`/api/tools/categories/${encodeURIComponent(categoryView.category)}/config`);
			if (res.ok) {
				const data = await res.json();
				let configStr = data.config || '{}';
				// Format JSON nicely
				try {
					const parsed = JSON.parse(configStr);
					const formatted = JSON.stringify(parsed, null, 2);
					setConfigJSON(formatted);

					// If category is Browser, populate browser form states
					if (categoryView.category.toLowerCase() === 'browser') {
						setBrowserEngine(parsed.engine || 'lightpanda');
						setBrowserHeadless(parsed.headless !== false); // default to true
						setLightpandaBinPath(parsed.lightpanda?.binPath || '');
						setLightpandaPort(parsed.lightpanda?.port || 9222);
						setChromiumBinPath(parsed.chromium?.binPath || '');
						setRemoteURL(parsed.remote?.url || '');
					} else if (categoryView.category.toLowerCase() === 'web') {
						setSearchProvider(parsed.search_provider || 'duckduckgo');
						setFetchProvider(parsed.fetch_provider || 'http');
						setUserAgent(parsed.user_agent || '');
						setTimeoutSeconds(parsed.timeout_seconds !== undefined ? parsed.timeout_seconds : 10);
						setMaxBytes(parsed.max_bytes !== undefined ? parsed.max_bytes : 1048576);
						setGoogleCX(parsed.google_cx || '');
						setWebLightpandaBinPath(parsed.lightpanda_bin_path || '');
					}
				} catch {
					setConfigJSON(configStr);
				}
				setShowConfigModal(true);
			} else {
				const err = await res.json();
				showToast(err.error || 'Failed to load category configuration', 'error');
			}
		} catch {
			showToast('Failed to load category configuration', 'error');
		}
	};

	const handleConfigSave = async (e: React.FormEvent) => {
		e.preventDefault();
		if (!activeCategory) return;

		let finalConfigJSON = configJSON;
		if (activeCategory.toLowerCase() === 'browser') {
			const browserConfigObj: any = {
				engine: browserEngine,
				headless: browserHeadless,
			};
			if (browserEngine === 'lightpanda') {
				browserConfigObj.lightpanda = {
					binPath: lightpandaBinPath || undefined,
					port: Number(lightpandaPort) || 9222,
				};
			} else if (browserEngine === 'chromium') {
				browserConfigObj.chromium = {
					binPath: chromiumBinPath || undefined,
				};
			} else if (browserEngine === 'remote') {
				browserConfigObj.remote = {
					url: remoteURL || undefined,
				};
			}
			finalConfigJSON = JSON.stringify(browserConfigObj);
		} else if (activeCategory.toLowerCase() === 'web') {
			const webConfigObj: any = {
				search_provider: searchProvider,
				fetch_provider: fetchProvider,
				user_agent: userAgent || undefined,
				timeout_seconds: Number(timeoutSeconds) || 10,
				max_bytes: Number(maxBytes) || 1048576,
				google_cx: googleCX || undefined,
				lightpanda_bin_path: webLightpandaBinPath || undefined,
			};
			finalConfigJSON = JSON.stringify(webConfigObj);
		}

		// Client-side JSON verification
		let parsedConfig = '';
		try {
			parsedConfig = JSON.stringify(JSON.parse(finalConfigJSON));
			setJsonError(null);
		} catch (err: any) {
			setJsonError(`Invalid JSON: ${err.message}`);
			return;
		}

		setSaving(true);
		try {
			const res = await fetch(`/api/tools/categories/${encodeURIComponent(activeCategory)}/config`, {
				method: 'PUT',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ config: parsedConfig }),
			});

			if (res.ok) {
				showToast(`Configuration for "${activeCategory}" saved successfully`, 'success');
				setShowConfigModal(false);
				loadTools();
			} else {
				const err = await res.json();
				setJsonError(err.error || 'Failed to save configuration');
				showToast(err.error || 'Failed to save configuration', 'error');
			}
		} catch {
			showToast('Failed to save configuration', 'error');
		} finally {
			setSaving(false);
		}
	};

	// Helper to get nice category icons or descriptions if needed
	const getCategoryDescription = (cat: string) => {
		switch (cat.toLowerCase()) {
			case 'filesystem':
				return 'Tools enabling the agent to read, write, and list files inside the workspace.';
			case 'shell':
				return 'Executes arbitrary shell commands inside the workspace directory.';
			default:
				return `Custom capability group: ${cat}.`;
		}
	};

	return (
		<div className="page-container">
			{/* Toolbar */}
			<div className="page-toolbar">
				<div className="page-toolbar-left">
					<span className="badge badge-inactive">
						{categories.reduce((acc, cat) => acc + cat.tools.length, 0)} tool(s) across {categories.length} categories
					</span>
				</div>
			</div>

			{/* Content */}
			<div style={{ padding: '20px 24px', overflowY: 'auto', flexGrow: 1 }}>
				{loading ? (
					<div className="empty-state">
						<p>Loading tools registry...</p>
					</div>
				) : categories.length === 0 ? (
					<div className="empty-state">
						<div className="empty-state-icon" aria-hidden="true">
							<Wrench size={40} weight="duotone" />
						</div>
						<h3>No tools registered</h3>
						<p>Could not find any standard or system tools registered.</p>
					</div>
				) : (
					<div style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
						{categories.map((catView) => (
							<div
								key={catView.category}
								style={{
									cursor: 'default',
									display: 'flex',
									flexDirection: 'column',
									border: '1px solid var(--border)',
									backgroundColor: 'var(--card)',
									padding: '16px 20px',
									borderRadius: '8px',
								}}
							>
								<div>
									{/* Category Header */}
									<div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '8px' }}>
										<h3 style={{ margin: 0, fontSize: '16px', fontWeight: 600, color: 'var(--foreground)' }}>
											{catView.category}
										</h3>
										{catView.configurable && (
											<button
												className="btn btn-secondary btn-sm"
												style={{
													display: 'flex',
													alignItems: 'center',
													gap: '4px',
													padding: '4px 8px',
													fontSize: '12px',
													border: '1px solid var(--border)',
													borderRadius: '6px',
													color: 'var(--foreground)',
													background: 'var(--bg-muted)',
													cursor: 'pointer',
												}}
												onClick={() => openConfig(catView)}
												title={`Configure ${catView.category}`}
											>
												<Gear size={14} /> Config
											</button>
										)}
									</div>
									<p style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '16px', lineHeight: '1.5' }}>
										{getCategoryDescription(catView.category)}
									</p>

									{/* Tools list inside category */}
									<div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
										{catView.tools.map((t) => (
											<div
												key={t.name}
												style={{
													display: 'flex',
													justifyContent: 'space-between',
													alignItems: 'center',
													padding: '8px 12px',
													backgroundColor: 'rgba(255, 255, 255, 0.03)',
													border: '1px solid rgba(255, 255, 255, 0.05)',
													borderRadius: '6px',
												}}
											>
												<div style={{ display: 'flex', flexDirection: 'column', flexGrow: 1, marginRight: '16px' }}>
													<span style={{ fontSize: '13px', fontWeight: 500, color: 'var(--foreground)' }}>
														{t.name}
													</span>
													{t.description && (
														<span style={{ fontSize: '11px', color: 'var(--text-muted)', marginTop: '2px', lineHeight: '1.4' }}>
															{t.description}
														</span>
													)}
												</div>
												<button
													type="button"
													style={{
														background: 'none',
														border: 'none',
														cursor: 'pointer',
														color: t.enabled ? 'var(--color-accent)' : 'var(--text-muted)',
														padding: '4px',
														display: 'flex',
														alignItems: 'center',
													}}
													onClick={() => toggleTool(t)}
													aria-label={t.enabled ? `Disable ${t.name}` : `Enable ${t.name}`}
												>
													{t.enabled ? (
														<ToggleRight size={28} weight="fill" />
													) : (
														<ToggleLeft size={28} weight="fill" />
													)}
												</button>
											</div>
										))}
									</div>
								</div>
							</div>
						))}
					</div>
				)}
			</div>

			{/* Configuration Modal */}
			{showConfigModal && activeCategory && (
				<div
					className="modal-overlay"
					onClick={(e) => { if (e.target === e.currentTarget) setShowConfigModal(false); }}
					role="dialog"
					aria-modal="true"
					aria-labelledby="config-modal-title"
				>
					<form className="modal-content" onSubmit={handleConfigSave} style={{ maxWidth: '600px' }} noValidate>
						<div className="modal-header">
							<h2 id="config-modal-title" className="modal-title" style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
								<Gear size={20} weight="bold" /> Configure {activeCategory}
							</h2>
							<button type="button" className="modal-close" onClick={() => setShowConfigModal(false)} aria-label="Close dialog">
								<X size={18} weight="bold" />
							</button>
						</div>

						{activeCategory.toLowerCase() === 'browser' ? (
							<div style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
								{/* Engine Selection */}
								<div className="form-group">
									<label className="form-label" htmlFor="browser-engine">
										Browser Engine
										<Tooltip content="Select the browser rendering engine. Lightpanda is a lightweight CDP subprocess; Chromium is standard Chrome; Remote connects to an existing CDP server." position="bottom" align="left" />
									</label>
									<select
										id="browser-engine"
										className="form-select"
										value={browserEngine}
										onChange={(e) => setBrowserEngine(e.target.value as any)}
									>
										<option value="lightpanda">Lightpanda (Embedded, low-memory)</option>
										<option value="chromium">Chromium (Full browser, high-fidelity)</option>
										<option value="remote">Remote CDP Server (Attach to running instance)</option>
									</select>
								</div>

								{/* Headless Mode (Chromium / Lightpanda) */}
								{browserEngine !== 'remote' && (
									<div className="form-group" style={{ display: 'flex', flexDirection: 'row', alignItems: 'center', gap: '8px' }}>
										<input
											id="browser-headless"
											type="checkbox"
											checked={browserHeadless}
											onChange={(e) => setBrowserHeadless(e.target.checked)}
											style={{ width: 'auto', margin: 0 }}
										/>
										<label htmlFor="browser-headless" style={{ margin: 0, fontWeight: 500, fontSize: '13px', cursor: 'pointer' }}>
											Headless Mode (Recommended)
										</label>
									</div>
								)}

								{/* Lightpanda specific inputs */}
								{browserEngine === 'lightpanda' && (
									<div style={{ display: 'flex', flexDirection: 'column', gap: '12px', padding: '12px', border: '1px solid var(--border)', borderRadius: '6px', backgroundColor: 'rgba(0,0,0,0.1)' }}>
										<div className="form-group">
											<label className="form-label" htmlFor="lp-bin-path">
												Lightpanda Binary Path
												<Tooltip content="Optional path to custom lightpanda executable (defaults to searching in PATH)." position="bottom" align="left" />
											</label>
											<input
												id="lp-bin-path"
												type="text"
												className="form-input"
												value={lightpandaBinPath}
												onChange={(e) => setLightpandaBinPath(e.target.value)}
												placeholder="e.g. /usr/local/bin/lightpanda"
											/>
										</div>
										<div className="form-group">
											<label className="form-label" htmlFor="lp-port">
												CDP Port
											</label>
											<input
												id="lp-port"
												type="number"
												className="form-input"
												value={lightpandaPort}
												onChange={(e) => setLightpandaPort(Number(e.target.value))}
												placeholder="9222"
											/>
										</div>
									</div>
								)}

								{/* Chromium specific inputs */}
								{browserEngine === 'chromium' && (
									<div style={{ display: 'flex', flexDirection: 'column', gap: '12px', padding: '12px', border: '1px solid var(--border)', borderRadius: '6px', backgroundColor: 'rgba(0,0,0,0.1)' }}>
										<div className="form-group">
											<label className="form-label" htmlFor="chrome-bin-path">
												Chromium/Chrome Binary Path
												<Tooltip content="Optional path to chromium binary (e.g. /usr/bin/chromium-browser or applications path)." position="bottom" align="left" />
											</label>
											<input
												id="chrome-bin-path"
												type="text"
												className="form-input"
												value={chromiumBinPath}
												onChange={(e) => setChromiumBinPath(e.target.value)}
												placeholder="e.g. /Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
											/>
										</div>
									</div>
								)}

								{/* Remote specific inputs */}
								{browserEngine === 'remote' && (
									<div style={{ display: 'flex', flexDirection: 'column', gap: '12px', padding: '12px', border: '1px solid var(--border)', borderRadius: '6px', backgroundColor: 'rgba(0,0,0,0.1)' }}>
										<div className="form-group">
											<label className="form-label" htmlFor="remote-url">
												Remote CDP Endpoint URL *
												<Tooltip content="Host address exposing Chrome DevTools protocol, e.g. http://127.0.0.1:9222" position="bottom" align="left" />
											</label>
											<input
												id="remote-url"
												type="text"
												className="form-input"
												value={remoteURL}
												onChange={(e) => setRemoteURL(e.target.value)}
												placeholder="e.g. http://127.0.0.1:9222"
												required
											/>
										</div>
									</div>
								)}
							</div>
						) : activeCategory.toLowerCase() === 'web' ? (
							<div style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
								{/* Search Provider Selection */}
								<div className="form-group">
									<label className="form-label" htmlFor="search-provider">
										Search Provider
										<Tooltip content="Select the search provider backend." position="bottom" align="left" />
									</label>
									<select
										id="search-provider"
										className="form-select"
										value={searchProvider}
										onChange={(e) => setSearchProvider(e.target.value)}
									>
										<option value="duckduckgo">DuckDuckGo (No key needed)</option>
										<option value="tavily">Tavily (API Key)</option>
										<option value="exa">Exa (API Key)</option>
										<option value="google">Google Custom Search (API Key + CX)</option>
									</select>
								</div>

								{/* Fetch Provider Selection */}
								<div className="form-group">
									<label className="form-label" htmlFor="fetch-provider">
										Fetch Provider
										<Tooltip content="Select the fetch provider backend." position="bottom" align="left" />
									</label>
									<select
										id="fetch-provider"
										className="form-select"
										value={fetchProvider}
										onChange={(e) => setFetchProvider(e.target.value)}
									>
										<option value="http">HTTP Client (Standard, low-memory)</option>
										<option value="lightpanda">Lightpanda CLI (Exec-based fetcher)</option>
									</select>
								</div>

								{/* Timeout Seconds */}
								<div className="form-group">
									<label className="form-label" htmlFor="web-timeout">
										Timeout (Seconds)
										<Tooltip content="Request timeout in seconds." position="bottom" align="left" />
									</label>
									<input
										id="web-timeout"
										type="number"
										className="form-input"
										value={timeoutSeconds}
										onChange={(e) => setTimeoutSeconds(Number(e.target.value))}
										placeholder="10"
									/>
								</div>

								{/* Max Bytes */}
								<div className="form-group">
									<label className="form-label" htmlFor="web-max-bytes">
										Max Response Bytes
										<Tooltip content="Maximum allowed response size in bytes." position="bottom" align="left" />
									</label>
									<input
										id="web-max-bytes"
										type="number"
										className="form-input"
										value={maxBytes}
										onChange={(e) => setMaxBytes(Number(e.target.value))}
										placeholder="1048576"
									/>
								</div>

								{/* User Agent */}
								<div className="form-group">
									<label className="form-label" htmlFor="web-user-agent">
										User Agent
										<Tooltip content="Custom HTTP User-Agent string." position="bottom" align="left" />
									</label>
									<input
										id="web-user-agent"
										type="text"
										className="form-input"
										value={userAgent}
										onChange={(e) => setUserAgent(e.target.value)}
										placeholder="e.g. Mozilla/5.0..."
									/>
								</div>

								{/* Google CX */}
								{searchProvider === 'google' && (
									<div className="form-group">
										<label className="form-label" htmlFor="web-google-cx">
											Google Custom Search Engine ID (CX) *
											<Tooltip content="Your Google Custom Search Engine CX ID." position="bottom" align="left" />
										</label>
										<input
											id="web-google-cx"
											type="text"
											className="form-input"
											value={googleCX}
											onChange={(e) => setGoogleCX(e.target.value)}
											placeholder="e.g. 0123456789abcdef0"
											required
										/>
									</div>
								)}

								{/* Lightpanda Bin Path */}
								{fetchProvider === 'lightpanda' && (
									<div className="form-group">
										<label className="form-label" htmlFor="web-lp-bin-path">
											Lightpanda Binary Path
											<Tooltip content="Optional path to custom lightpanda executable." position="bottom" align="left" />
										</label>
										<input
											id="web-lp-bin-path"
											type="text"
											className="form-input"
											value={webLightpandaBinPath}
											onChange={(e) => setWebLightpandaBinPath(e.target.value)}
											placeholder="e.g. /usr/local/bin/lightpanda"
										/>
									</div>
								)}
							</div>
						) : (
							<>
								<div className="form-group">
									<label className="form-label" htmlFor="category-config">
										Configuration JSON
										<Tooltip content="Edit category settings in JSON format. Validated against the registered schema." position="bottom" align="left" />
									</label>
									<textarea
										id="category-config"
										className={`form-input ${(jsonError) ? 'is-invalid' : ''}`}
										style={{
											fontFamily: 'monospace',
											fontSize: '12px',
											minHeight: '240px',
											resize: 'vertical',
											backgroundColor: 'rgba(0,0,0,0.2)',
											color: '#e2e8f0',
											lineHeight: '1.5',
										}}
										value={configJSON}
										onChange={(e) => {
											setConfigJSON(e.target.value);
											setJsonError(null);
										}}
										placeholder="{}"
										required
									/>
									{jsonError && (
										<span className="form-error" style={{ display: 'block', marginTop: '4px' }}>{jsonError}</span>
									)}
								</div>

								{configSchema && (
									<div style={{ marginTop: '16px', padding: '12px', borderRadius: '6px', backgroundColor: 'rgba(0,0,0,0.15)', border: '1px solid var(--border)' }}>
										<span style={{ fontSize: '11px', fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase' }}>Config Schema</span>
										<pre style={{ margin: '8px 0 0 0', fontSize: '11px', fontFamily: 'monospace', color: 'var(--text-muted)', overflowX: 'auto', maxHeight: '120px' }}>
											{configSchema}
										</pre>
									</div>
								)}
							</>
						)}

						<div className="modal-footer" style={{ marginTop: '24px', display: 'flex', justifyContent: 'flex-end', gap: '12px' }}>
							<button
								type="button"
								className="btn btn-secondary"
								onClick={() => setShowConfigModal(false)}
								disabled={saving}
							>
								Cancel
							</button>
							<button
								type="submit"
								className="btn btn-primary"
								disabled={saving}
							>
								{saving ? 'Saving...' : 'Save Config'}
							</button>
						</div>
					</form>
				</div>
			)}
		</div>
	);
}
