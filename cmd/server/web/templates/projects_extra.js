
        editConfigFile(comp) {
            this.editingConfigComponent = comp;
            this.configFileContent = '';
            this.showConfigModal = true;
        },

        saveConfigFile() {
            this.showToast('配置文件功能开发中', 'success');
            this.showConfigModal = false;
        }
