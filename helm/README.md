# Dell Powermax Helm Chart for Kubernetes

For detailed installation instructions, please check the `dell-csi-helm-installer` directory

The general outline is:

    1. Satisfy the pre-requsites outlined in the Release and Installation Notes in the doc directory.

    2. Create a Kubernetes secret with the PowerMax credentials using the template in secret.yaml.

    3. Make a copy of the `csi-powermax/values.yaml` to the location of your choice(csi-powermax/myvalues.yaml) and fill in various installation parameters.

    4. Run the helm install command, first using the dry-run flag to confirm various parameters are as desired.
       Ex: helm install --dry-run --values ./csi-powermax/myvalues.yaml --namespace powermax powermax ./csi-powermax"

    5. Or Invoke the `dell-csi-helm-installer/csi-install.sh` shell script which deploys the helm chart for CSI PowerMax driver.
