// SPDX-License-Identifier: AGPL-3.0-or-later
const path=require('node:path');
const metadata=require('../package.json');
const fs=require('node:fs/promises');
const {spawnSync}=require('node:child_process');
const {downloadAndUnzipVSCode,resolveCliArgsFromVSCodeExecutablePath,runTests}=require('@vscode/test-electron');
(async()=>{
 const root=path.resolve(__dirname,'../../..'),fixture=path.join(root,'dist/vscode-issue74');
 await fs.mkdir(path.join(fixture,'.vscode'),{recursive:true});
 await fs.writeFile(path.join(fixture,'.vscode/settings.json'),JSON.stringify({'cooler.dapPath':path.join(root,'dist/cooler-dap.exe'),'cooler.lspPath':path.join(root,'dist/cooler-lsp-issue74.exe'),'cooler.compilerPath':path.join(root,'dist/coolerc-issue74.exe'),'cooler.sourceSet':{include:['main.cl']},'extensions.ignoreRecommendations':true}));
 await fs.writeFile(path.join(fixture,'main.cl'),'class Main inherits IO {main():Object {out_int(42)};};');
 await fs.mkdir(path.join(fixture,'other-exercises'),{recursive:true});
 await Promise.all(Array.from({length:1025},(_,i)=>fs.writeFile(path.join(fixture,'other-exercises',`${i}.cl`),'class Other {};')));
 await fs.writeFile(path.join(fixture,'main-debug.cl'),'class Main inherits IO {\n main():Object { {\n out_int((new Helper).inc(7));\n out_int(in_int());\n } };\n};');
 await fs.writeFile(path.join(fixture,'helper-debug.cl'),'class Helper {\n inc(n:Int):Int { n+1 };\n};');
 await fs.writeFile(path.join(fixture,'loop-debug.cl'),'class Main { main():Object { while true loop 0 pool }; };');
 const executable=await downloadAndUnzipVSCode({version:'1.136.1',cachePath:path.join(root,'dist/vscode-download')});
 const extensions=path.join(root,'dist/vscode-installed'),user=path.join(root,'dist/vscode-user');
 const [cli,...args]=resolveCliArgsFromVSCodeExecutablePath(executable);
 let command=cli,cliArgs=args,env=process.env;
 if(process.platform==='win32'){
  const launcher=await fs.readFile(cli,'utf8');const match=launcher.match(/"%~dp0([^"\r\n]*cli\.js)"/u);if(!match)throw new Error('Unrecognized VS Code CLI launcher');
  command=executable;cliArgs=[path.resolve(path.dirname(cli),match[1])];env={...process.env,ELECTRON_RUN_AS_NODE:'1'};
 }
 const installed=spawnSync(command,[...cliArgs,'--extensions-dir',extensions,'--user-data-dir',user,'--install-extension',path.join(root,'dist/cooler-local.vsix'),'--force'],{stdio:'inherit',shell:false,env,windowsHide:true,timeout:30000});
 if(installed.status!==0)throw new Error('VSIX installation failed');
 const timeout=setTimeout(()=>{console.error('VS Code host exceeded 90 seconds');process.exit(1);},90000);
 try {await runTests({vscodeExecutablePath:executable,extensionDevelopmentPath:path.join(extensions,`${metadata.publisher}.${metadata.name}-${metadata.version}`),extensionTestsPath:path.join(__dirname,'suite.cjs'),launchArgs:[fixture,'--extensions-dir',extensions,'--user-data-dir',user,'--disable-updates','--skip-welcome','--disable-telemetry'],extensionTestsEnv:{COOLER_TEST_WORKSPACE:fixture}});}finally{clearTimeout(timeout);}
})().catch(error=>{console.error(error);process.exitCode=1;});
