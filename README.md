# mark

      mark add *.css
     
      mark
      # oh look a bunch of css files_
     
      # in another window*
      mark exec cp _ static/css
      ls static/css
      # oh look a bunch of css files
     
      mark
      # what no more css files
     
      mark add *.css ; cd ../more-css ; mark add *.css
      mark tag css
      cd ../js ; mark add *.js
      mark tag js *.js
     
      # in another window
      mark -tag js exec cp _ static/js
      ls static/js
      # oh look a bunch of js files
     
      mark
      # removing them is your problem
     
      mark remove *.js
      mark
      # whew
     

