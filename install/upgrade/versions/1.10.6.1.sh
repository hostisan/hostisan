#!/bin/bash

# Hostisan upgrade step 1.10.6.1 (runs once: UPGRADE_LAST_STEP)

#######################################################################################
#######                      Place additional commands below.                   #######
#######################################################################################
####### upgrade_config_set_value only accepts true or false.                    #######
#######                                                                         #######
####### Pass through information to the end user in case of a issue or problem  #######
#######                                                                         #######
####### Use add_upgrade_message "My message here" to include a message          #######
####### in the upgrade notification email. Example:                             #######
#######                                                                         #######
####### add_upgrade_message "My message here"                                   #######
#######                                                                         #######
####### You can use \n within the string to create new lines.                   #######
#######################################################################################

upgrade_config_set_value 'UPGRADE_UPDATE_WEB_TEMPLATES' 'false'
upgrade_config_set_value 'UPGRADE_UPDATE_DNS_TEMPLATES' 'false'
upgrade_config_set_value 'UPGRADE_UPDATE_FILEMANAGER_CONFIG' 'false'
upgrade_config_set_value 'UPGRADE_UPDATE_MAIL_TEMPLATES' 'false'
upgrade_config_set_value 'UPGRADE_REBUILD_USERS' 'false'

# Updates from Git are refused in Hostisan (v-update-sys-hestia-git). A cron job
# left by "v-update-sys-hestia-git ... install-auto" (v-add-cron-hestia-autoupdate
# git) would fail every night: replace it with the apt autoupdate.
# v-add-cron-hestia-autoupdate refuses with the cron system off or in demo mode,
# so then the line stays and the admin is told; it counts as replaced only when
# the apt line is in the crontab afterwards.
hestiaweb_crontab=/var/spool/cron/crontabs/hestiaweb
if [ -f "$hestiaweb_crontab" ] && grep -q 'v-update-sys-hestia-git' "$hestiaweb_crontab"; then
	if [ -z "$CRON_SYSTEM" ] || [ "$DEMO_MODE" = "yes" ]; then
		echo "[ ! ] The Git autoupdate cron job could not be replaced (cron system off or demo mode)."
		$BIN/v-log-action "system" "Warning" "Updates" "Git autoupdate cron job could not be replaced with the apt autoupdate (cron system off or demo mode); updates from Git are not supported in Hostisan."
		add_upgrade_message "The automatic update from Git could not be replaced with the apt automatic update, because the cron system is off or demo mode is on. Updates from Git are not supported in Hostisan, so that job fails every night: turn automatic updates off and on again in the server settings once you can."
	else
		echo "[ * ] Replacing the Git autoupdate cron job with the apt autoupdate..."
		sed -i '/v-update-sys-hestia-git/d' "$hestiaweb_crontab"
		$BIN/v-add-cron-hestia-autoupdate apt
		if grep -q 'v-update-sys-hestia-all' "$hestiaweb_crontab"; then
			$BIN/v-log-action "system" "Info" "Updates" "Git autoupdate cron job replaced with the apt autoupdate (updates from Git are not supported in Hostisan)."
			add_upgrade_message "The automatic update from Git was replaced with the apt automatic update: updates from Git are not supported in Hostisan."
		else
			echo "[ ! ] The Git autoupdate cron job was removed, but the apt autoupdate could not be added."
			$BIN/v-log-action "system" "Warning" "Updates" "Git autoupdate cron job removed (updates from Git are not supported in Hostisan); the apt autoupdate could not be added."
			add_upgrade_message "The automatic update from Git was removed, because updates from Git are not supported in Hostisan, and the apt automatic update could not be added. Automatic updates are off: turn them on again in the server settings."
		fi
	fi
fi
unset hestiaweb_crontab
